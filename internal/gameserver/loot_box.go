package gameserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// ---- 钓获礼包（开箱） ----
//
// 玩家钓上鱼并入护后，按概率触发"礼包"，服务端当场掷骰开箱：
// 选出奖励 → 直接进背包 → 推送物品更新 → 聊天播报"开到了什么"。
//
// 设计取舍：客户端没有可用的"开箱弹窗"协议（10304 奖励包只有字段表、
// 服务端零实现），因此不走新增 opcode，而是复用已有的
// AddToStack + pushItemUpdate + 聊天播报，做到"钓上来直接开到东西"的体感。
//
// 配置写在 kv.loot_box_defs（JSON），改完 30 秒内生效，不用重启：
//
//	[{"id":"event_box","name":"活动礼包","chance":0.05,
//	  "rewards":[{"item_id":"worm_01","qty":2,"weight":10}]}]
//
// rewards 留空时，从商店货架随机抽一件有库存的商品（用户要求的"随机商店物品"）。

const lootBoxDefsKVKey = "loot_box_defs"

// disableLootBoxRandomReward 硬切换钓获礼包的奖励内容：
//
// 为 true（生产默认）时，开箱命中后不再按权重/商店随机抽物品，改为发放
// 随机金币分（profile.gold += N，项目经济口径为金币百分位，100=1 金）。
// N 取自截断正态分布：范围默认 1~200 分（0.01~2 金）、集中度 σ 默认 30、
// 期望校准为 ~100 分（1 金）。范围/集中度/期望均可后台调整。
// 触发条件仍是 loot_box_enabled 总开关 + loot_box_chance 概率 + 上星保底
// （蓝冠必保底、奖杯走独立概率），只是"开出什么"从随机物品变成随机金币。
//
// 该变量只在 *_test.go 中被临时置 false 以验证原随机抽取机制仍然完整；生产
// 运行时恒为 true。需要恢复随机物品奖励时把初值改为 false 即可，无需删代码。
var disableLootBoxRandomReward = true

// 随机金币的默认参数。对应后台 tunables（改后约 30 秒生效）：
//
//	loot_box_gold_lo     下限（分，默认 1，最小 1）
//	loot_box_gold_hi     上限（分，默认 200）
//	loot_box_gold_sigma  集中度 σ（默认 30，越小越集中在期望附近）
//	loot_box_gold_mean   目标期望（分，默认 100）
//
// 取值范围钳制在 [lo, hi]，σ 下限 1e-3 防除零，区间上限 100000 分防病态配置。
const (
	lootGoldDefaultLo    = 1
	lootGoldDefaultHi    = 200
	lootGoldDefaultSigma = 30
	lootGoldDefaultMean  = 100
	lootGoldMaxSpan      = 100000
)

// lootGoldConfig 一次抽奖的已校验参数。
type lootGoldConfig struct {
	lo, hi     int64
	sigma      float64
	targetMean float64
}

// lootBoxGoldConfig 读取并钳制后台可调的金币抽奖参数。
func (s *Server) lootBoxGoldConfig() lootGoldConfig {
	lo := int64(s.tunableF("loot_box_gold_lo", lootGoldDefaultLo))
	hi := int64(s.tunableF("loot_box_gold_hi", lootGoldDefaultHi))
	sigma := s.tunableF("loot_box_gold_sigma", lootGoldDefaultSigma)
	target := s.tunableF("loot_box_gold_mean", lootGoldDefaultMean)
	if lo < 1 {
		lo = 1
	}
	if hi < lo {
		hi = lo
	}
	if hi-lo > lootGoldMaxSpan {
		hi = lo + lootGoldMaxSpan
	}
	if sigma < 1e-3 || math.IsNaN(sigma) || math.IsInf(sigma, 0) {
		sigma = lootGoldDefaultSigma
	}
	if target < float64(lo) {
		target = float64(lo)
	}
	if target > float64(hi) {
		target = float64(hi)
	}
	if math.IsNaN(target) || math.IsInf(target, 0) {
		target = float64(lo+hi) / 2
	}
	return lootGoldConfig{lo: lo, hi: hi, sigma: sigma, targetMean: target}
}

// drawLootGold 按截断正态分布抽一个金币分数。
//
// 算法（与用户提供的 Python 原型等价）：
//  1. 权重 w(x) = exp(-(x-μ)² / (2σ²))，x ∈ [lo,hi]；
//  2. 二分校准 μ，使加权期望严格 ≈ 目标期望（截断会削弱中心，故需校准）；
//  3. 用校准后的权重算累积分布 CUM，随机 r∈[0,CUM末) 经二分查找定位 x。
//
// 这样金额集中在期望附近、两端稀少，且均值可精确控制。
func drawLootGold(cfg lootGoldConfig) int64 {
	n := cfg.hi - cfg.lo + 1
	if n <= 1 {
		return cfg.lo
	}
	// weights 返回各点的正态权重（未归一化）。
	weights := func(mu float64) []float64 {
		w := make([]float64, n)
		inv := 1 / (2 * cfg.sigma * cfg.sigma)
		for i := int64(0); i < n; i++ {
			d := float64(cfg.lo+i) - mu
			w[i] = math.Exp(-d * d * inv)
		}
		return w
	}
	// expectedMean 给定 μ 时的加权期望。
	expectedMean := func(mu float64) float64 {
		w := weights(mu)
		var sum, acc float64
		for i, wi := range w {
			sum += wi
			acc += float64(cfg.lo+int64(i)) * wi
		}
		if sum <= 0 {
			return cfg.targetMean // 病态兜底：让二分停在当前位置
		}
		return acc / sum
	}
	// 二分校准 μ：期望随 μ 单调递增，80 次迭代足够收敛到浮点精度。
	a, b := float64(cfg.lo), float64(cfg.hi)
	for i := 0; i < 80; i++ {
		mid := (a + b) / 2
		if expectedMean(mid) < cfg.targetMean {
			a = mid
		} else {
			b = mid
		}
	}
	mu := (a + b) / 2
	w := weights(mu)
	cum := make([]float64, n)
	var acc float64
	for i, wi := range w {
		acc += wi
		cum[i] = acc
	}
	if acc <= 0 {
		return int64(cfg.targetMean) // 理论不可达，防御
	}
	r := rand.Float64() * acc
	idx := sort.Search(len(cum), func(i int) bool { return cum[i] >= r })
	if idx >= len(cum) {
		idx = len(cum) - 1
	}
	return cfg.lo + int64(idx)
}

// lootBoxReward 礼包里的单个奖励。
type lootBoxReward struct {
	ItemID string  `json:"item_id"`
	Qty    int64   `json:"qty"`
	Weight float64 `json:"weight"`
}

// lootBoxDef 一种礼包。
type lootBoxDef struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Chance  float64         `json:"chance"` // 每次钓获触发的概率 0..1
	Rewards []lootBoxReward `json:"rewards"`
	// CarrierItem 可选：开袋时额外发放的"可见载体"物品。
	// category 73 的 gift_bag 没有 wire class 映射，直接发会让客户端物化出错，
	// 因此默认不发；需要"背包里看得见一个袋子"时，把这里填成可正常显示的道具
	// （例如 category 55 的套盒）。
	CarrierItem string `json:"carrier_item"`
	CarrierQty  int64  `json:"carrier_qty"`
}

// defaultLootBoxDefs 内置兜底：5% 概率钓到"活动礼包"，随机开出一件商店商品。
var defaultLootBoxDefs = []lootBoxDef{
	{ID: "event_box", Name: "福袋", Chance: 0.05},
}

// lootBoxDefs 读当前生效的礼包表：优先 kv.loot_box_defs，解析失败用内置兜底。
func (s *Server) lootBoxDefs(ctx context.Context) []lootBoxDef {
	if s.DB != nil {
		var raw string
		if err := s.DB.SQL.QueryRowContext(ctx, `SELECT v FROM kv WHERE k=?`, lootBoxDefsKVKey).Scan(&raw); err == nil && raw != "" {
			var defs []lootBoxDef
			if json.Unmarshal([]byte(raw), &defs) == nil {
				return defs
			}
			s.log.Warn("礼包配置解析失败，使用内置兜底", "kv", lootBoxDefsKVKey)
		}
	}
	return defaultLootBoxDefs
}

// rollLootBox 在钓获入护后尝试触发礼包；命中则发奖并播报。
// 返回开出的物品 ID（未触发为空）。
func (s *Server) rollLootBox(ctx context.Context, sess *Session, catch *RolledFish) string {
	if s == nil || s.DB == nil || sess == nil {
		return ""
	}
	// 总开关：默认关闭，需要管理员在后台显式开启后才触发。
	if s.tunableF("loot_box_enabled", 0) != 1 {
		return ""
	}
	defs := s.lootBoxDefs(ctx)
	if len(defs) == 0 {
		return ""
	}
	// 蓝冠保底：钓到蓝冠(4)鱼时必定开一包，不受概率影响。
	// 由 tunables.loot_box_trophy_guarantee 控制（默认开启，0=关）。
	grade := catchGrade(catch)
	if grade >= 4 && s.tunableF("loot_box_trophy_guarantee", 1) == 1 {
		if itemID := s.grantLootBox(ctx, sess, defs[0], catch, "保底"); itemID != "" {
			return itemID
		}
	}
	// 奖杯(上星)概率：钓到上星(3)鱼时单独掷一次概率（与蓝冠保底分开），
	// 由 tunables.loot_box_trophy_chance 控制（默认 0.5，0=关，1=必开）。
	// 上星只走这一支：命中开一包，未命中不再叠加通用概率，这样把概率调 0
	// 就能彻底关掉上星开箱、调 1 则必开，概率值精确可控。
	if grade == 3 {
		if chance := s.lootBoxTrophyChanceForSession(sess); chance > 0 && rand.Float64() < chance {
			if itemID := s.grantLootBox(ctx, sess, defs[0], catch, "奖杯概率"); itemID != "" {
				return itemID
			}
		}
		return ""
	}
	// 用户修正：福袋只在上星(3)/蓝冠(4)开；合格(2)及以下不开。
	// 原实现让合格鱼落到下方通用概率分支，导致"合格开福袋、上星蓝冠反而没有"。
	if grade < 3 {
		return ""
	}
	// 一次钓获最多开一个礼包：按顺序判定，命中即停。
	for _, def := range defs {
		chance := s.lootBoxChance(def)
		if chance <= 0 || rand.Float64() >= chance {
			continue
		}
		if itemID := s.grantLootBox(ctx, sess, def, catch, "随机"); itemID != "" {
			return itemID
		}
	}
	return ""
}

// lootBoxChance 取该礼包的触发概率：tunables.loot_box_chance 优先（后台可调），
// 否则用配置里的 chance。负数视为关闭。
func (s *Server) lootBoxChance(def lootBoxDef) float64 {
	p := s.tunableF("loot_box_chance", -1)
	if p < 0 {
		return def.Chance
	}
	if p > 1 {
		return 1
	}
	return p
}

// lootBoxTrophyChance 取上星鱼单独开箱的概率：tunables.loot_box_trophy_chance
// （后台可调，默认 0.5）。0=关闭上星概率触发；超过 1 视为必开。
// 与 lootBoxChance（普通鱼通用概率）相互独立。
func (s *Server) lootBoxTrophyChance() float64 {
	p := s.tunableF("loot_box_trophy_chance", 0.5)
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}

// lootBoxTrophyChanceForSession 上星鱼开箱概率：欢乐时光进行中提升到 80%，
// 结束后立即回落到后台配置值（每次掷骰实时读取欢乐时光状态，无缓存）。
func (s *Server) lootBoxTrophyChanceForSession(sess *Session) float64 {
	if sess != nil {
		sess.happyMu.Lock()
		active := sess.happyStatus.Active
		sess.happyMu.Unlock()
		if active {
			if v := s.tunableF("loot_box_happy_trophy_chance", 0.8); v >= 0 {
				if v > 1 {
					return 1
				}
				return v
			}
		}
	}
	return s.lootBoxTrophyChance()
}

// catchGrade 安全取鱼档位（1 未达标 / 2 合格 / 3 奖杯 / 4 蓝冠）。
func catchGrade(f *RolledFish) int64 {
	if f == nil {
		return 0
	}
	return f.Grade
}

// lootWireCategoryEligible 只允许能由权威 categoryWireClass 正确物化的类别。
func lootWireCategoryEligible(category int64) bool {
	_, ok := categoryWireClass[category]
	return ok
}

// lootRewardCategoryEligible 在 wire 可物化基础上再排除套盒/礼物袋，避免奖励套娃。
func lootRewardCategoryEligible(category int64) bool {
	if category == 55 || category == 73 {
		return false
	}
	return lootWireCategoryEligible(category)
}

func (s *Server) lootItemCategory(ctx context.Context, itemID string) (int64, bool) {
	def, err := s.DB.GetItemDefByItemID(ctx, itemID)
	if err != nil || def == nil {
		s.log.Warn("礼包物品定义不存在", "item", itemID, "err", err)
		return 0, false
	}
	return def.Category, true
}

func (s *Server) lootRewardItemEligible(ctx context.Context, itemID string) bool {
	category, ok := s.lootItemCategory(ctx, itemID)
	if !ok {
		return false
	}
	if !lootRewardCategoryEligible(category) {
		s.log.Warn("礼包奖励类别不可发放", "item", itemID, "category", category)
		return false
	}
	return true
}

func (s *Server) lootCarrierItemEligible(ctx context.Context, itemID string) bool {
	category, ok := s.lootItemCategory(ctx, itemID)
	if !ok {
		return false
	}
	if !lootWireCategoryEligible(category) {
		s.log.Warn("礼包载体类别不可物化", "item", itemID, "category", category)
		return false
	}
	return true
}

// grantLootBox 命中礼包后发放奖励，失败返回空串。
// 生产默认走随机金币（disableLootBoxRandomReward=true）；关闭开关才回退到原随机物品。
func (s *Server) grantLootBox(ctx context.Context, sess *Session, def lootBoxDef, catch *RolledFish, reason string) string {
	if disableLootBoxRandomReward {
		return s.grantLootBoxGold(ctx, sess, def, catch, reason)
	}
	return s.grantLootBoxRandomItem(ctx, sess, def, catch, reason)
}

// grantLootBoxGold 随机金币发奖：按截断正态分布抽一个分数，写入档案与一条
// 流水，刷新客户端档案并聊天播报。返回 "gold" 表示成功（供上层判断是否命中）。
func (s *Server) grantLootBoxGold(ctx context.Context, sess *Session, def lootBoxDef, catch *RolledFish, reason string) string {
	if s == nil || s.DB == nil || sess == nil {
		return ""
	}
	amount := drawLootGold(s.lootBoxGoldConfig())
	tx, err := beginEconomyTx(ctx, s.DB)
	if err != nil {
		s.log.Warn("钓获礼包发金币开启事务失败", "uid", sess.Userid, "box", def.ID, "err", err)
		return ""
	}
	defer func() { _ = tx.Rollback() }()
	note := "钓获礼包:" + def.ID
	if err := applyWalletTx(ctx, tx, sess.Userid, 0, amount,
		"loot_box", "", "", note); err != nil {
		s.log.Warn("钓获礼包发金币失败", "uid", sess.Userid, "box", def.ID, "err", err)
		return ""
	}
	if err := tx.Commit(); err != nil {
		s.log.Warn("钓获礼包发金币提交失败", "uid", sess.Userid, "box", def.ID, "err", err)
		return ""
	}
	// 金币变化后刷新档案（2/1 的 f8 携带 gold），无 tunnel 的测试会话安全返回。
	sess.Push2_1()
	// 用户修正（2026-11-18）：播报格式一字不改，只把中间金币数值与来源标签替换。
	//   奖杯(grade3) → 获得：x金币（奖杯概率）
	//   蓝冠(grade4) → 获得：x金币（蓝冠必得）
	tag := ""
	if reason == "保底" {
		tag = "蓝冠必得"
	} else if reason == "奖杯概率" {
		tag = "奖杯概率"
	}
	// gold 为百分位：amount 分 = amount/100 金，展示两位小数。
	if tag != "" {
		sess.pushSystemChat(fmt.Sprintf("获得：%.2f金币（%s）", float64(amount)/100, tag))
	} else {
		sess.pushSystemChat(fmt.Sprintf("获得：%.2f金币", float64(amount)/100))
	}
	s.log.Info("钓获礼包开出金币", "uid", sess.Userid, "box", def.ID, "gold", amount,
		"reason", reason, "fish", catchSpecies(catch), "grade", catchGrade(catch))
	return "gold"
}

// grantLootBoxRandomItem 原始随机抽物品发奖逻辑（关闭 disableLootBoxRandomReward 时启用）。
func (s *Server) grantLootBoxRandomItem(ctx context.Context, sess *Session, def lootBoxDef, catch *RolledFish, reason string) string {
	itemID, qty, name := s.pickLootReward(ctx, def)
	if itemID == "" {
		return ""
	}
	rows, err := s.grantItemUnits(ctx, sess.Userid, itemID, "backpack", qty)
	if err != nil {
		s.log.Warn("礼包发奖失败", "uid", sess.Userid, "box", def.ID, "item", itemID, "err", err)
		return ""
	}
	for _, row := range rows {
		s.pushItemUpdate(sess.Userid, row)
	}
	// 可选载体：让背包里出现一个看得见的"袋子"。
	if def.CarrierItem != "" && def.CarrierItem != itemID {
		qty := def.CarrierQty
		if qty <= 0 {
			qty = 1
		}
		if !s.lootCarrierItemEligible(ctx, def.CarrierItem) {
			s.log.Warn("福袋载体已跳过", "uid", sess.Userid, "carrier", def.CarrierItem)
		} else if crows, cerr := s.grantItemUnits(ctx, sess.Userid, def.CarrierItem, "backpack", qty); cerr == nil {
			for _, crow := range crows {
				s.pushItemUpdate(sess.Userid, crow)
			}
		} else {
			s.log.Warn("福袋载体发放失败", "uid", sess.Userid, "carrier", def.CarrierItem, "err", cerr)
		}
	}
	display := name
	if display == "" {
		display = itemID
	}
	prefix := "钓到了" + def.Name
	if reason == "保底" {
		prefix = "上星保底：" + def.Name
	}
	sess.pushSystemChat(fmt.Sprintf("%s，开出：%s ×%d", prefix, display, qty))
	s.log.Info("礼包开出", "uid", sess.Userid, "box", def.ID, "item", itemID, "qty", qty,
		"reason", reason, "fish", catchSpecies(catch), "grade", catchGrade(catch))
	return itemID
}

// catchSpecies 安全取鱼种名（catch 可能为 nil）。
func catchSpecies(f *RolledFish) string {
	if f == nil {
		return ""
	}
	return f.Species
}

// pickLootReward 选出奖励：配置了 rewards 就按权重抽，否则从商店货架随机一件。
func (s *Server) pickLootReward(ctx context.Context, def lootBoxDef) (itemID string, qty int64, name string) {
	if len(def.Rewards) > 0 {
		eligible := make([]lootBoxReward, 0, len(def.Rewards))
		var total float64
		for _, r := range def.Rewards {
			if r.ItemID == "" || r.Weight <= 0 || !s.lootRewardItemEligible(ctx, r.ItemID) {
				continue
			}
			eligible = append(eligible, r)
			total += r.Weight
		}
		if total > 0 {
			x := rand.Float64() * total
			for _, r := range eligible {
				if x < r.Weight {
					q := r.Qty
					if q <= 0 {
						q = 1
					}
					return r.ItemID, q, s.lootItemName(ctx, r.ItemID)
				}
				x -= r.Weight
			}
		}
	}
	return s.pickRandomShopItem(ctx)
}

// pickRandomShopItem 从有库存的商店物品中等概率抽一件。
// 排除套盒/礼包类（55/73），并要求类别存在权威 wire 映射，避免错误物化。
func (s *Server) pickRandomShopItem(ctx context.Context) (itemID string, qty int64, name string) {
	rows, err := s.DB.SQL.QueryContext(ctx, `
		SELECT DISTINCT s.item_id, d.category
		FROM shop_item s
		JOIN item_def d ON d.item_id = s.item_id
		WHERE s.stock > 0
		ORDER BY s.item_id`)
	if err != nil {
		s.log.Warn("礼包抽取商店物品失败", "err", err)
		return "", 0, ""
	}
	defer rows.Close()
	var candidates []string
	for rows.Next() {
		var candidate string
		var category int64
		if err := rows.Scan(&candidate, &category); err != nil {
			continue
		}
		if lootRewardCategoryEligible(category) {
			candidates = append(candidates, candidate)
		}
	}
	if err := rows.Err(); err != nil {
		s.log.Warn("礼包抽取商店物品失败", "err", err)
		return "", 0, ""
	}
	if len(candidates) == 0 {
		s.log.Warn("礼包抽取商店物品失败", "err", "没有可发放的商品")
		return "", 0, ""
	}
	itemID = candidates[rand.Intn(len(candidates))]
	return itemID, 1, s.lootItemName(ctx, itemID)
}

// lootItemName 取物品中文名（缺省回退 item_id）。
func (s *Server) lootItemName(ctx context.Context, itemID string) string {
	var name string
	if err := s.DB.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(NULLIF(name_zh,''), NULLIF(name,''), ?) FROM item_catalog WHERE item_id=? LIMIT 1`,
		itemID, itemID).Scan(&name); err != nil || strings.TrimSpace(name) == "" {
		return itemID
	}
	return name
}

// scheduleLootBox 异步开箱：不阻塞 14/5 入护回包，也不受请求 ctx 取消影响。
func (s *Server) scheduleLootBox(sess *Session, catch *RolledFish) {
	if s == nil || sess == nil || s.DB == nil {
		return
	}
	fish := catch
	if fish != nil {
		cp := *fish
		fish = &cp
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Warn("礼包开箱 panic", "uid", sess.Userid, "panic", r)
			}
		}()
		ctx, cancel := context.WithTimeout(s.dbCtx(), 30*time.Second)
		defer cancel()
		s.rollLootBox(ctx, sess, fish)
	}()
}
