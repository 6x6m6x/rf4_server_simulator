package gameserver

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"

	"rf4server/internal/store"
)

// 预生成固定候选池：撇开 cafe_reward_templates.json，从 loc_fish（可钓图）取鱼种、
// item_catalog 取中文名、fish_grade_lines 取档位线作为重量要求，按每图特色生成候选订单。
//
// 10 槽位分配：
//
//	slot 0,1  奖杯位（2 位）——重量取奖杯线，奖杯档赏金
//	slot 2    长期位（1 位）——稀有鱼/巨物白名单，重量取蓝冠线，稀有奖杯/蓝冠赏金 + 巨物补贴
//	slot 3-9  普通/达标位（7 位）——重量取达标线，普通/达标赏金
//
// 每个槽位预生成多条候选（variant），运行时按现实时间周期轮换，同一周期内稳定。

const (
	cafePoolVariants   = 4   // 每槽位候选条数
	cafeBountyCapCents = 10000 // 巨物补贴后封顶 100 金
)

// cafeBountyBandKey 各槽位、鱼种稀缺对应的区间键（与迁移 00071 一致）。
// 长期位走蓝冠区间（奖励结构同蓝冠：稀有奖杯/蓝冠），普通位走普通/达标区间。
func cafeBountyBandKey(kind string, isRare bool) string {
	prefix := kind
	if kind == "longterm" {
		prefix = "blue"
	}
	rare := "common"
	if isRare {
		rare = "rare"
	}
	return prefix + "_" + rare
}

// cafeSlotMixDefaults 槽位占比缺省值：2 奖杯 + 1 长期 + 7 普通 = 10。
func cafeSlotMixDefaults() map[string]int {
	return map[string]int{"trophy": 2, "longterm": 1, "normal": 7}
}

// cafeSlotMixMap 用后台配置覆盖缺省占比；三类总和非法（!=10）时回退缺省，避免生成失败。
func cafeSlotMixMap(cfg store.CafeBountyConfig) map[string]int {
	m := cafeSlotMixDefaults()
	if len(cfg.SlotMix) == 0 {
		return m
	}
	total := 0
	in := map[string]int{}
	for _, s := range cfg.SlotMix {
		if s.Kind != "trophy" && s.Kind != "longterm" && s.Kind != "normal" {
			return m
		}
		if s.Count < 0 || s.Count > 10 {
			return m
		}
		in[s.Kind] = s.Count
		total += s.Count
	}
	if total != 10 {
		return m
	}
	for k, v := range in {
		m[k] = v
	}
	return m
}

// cafeSlotKinds 按占比展开为 slot -> kind 序列：奖杯位在前，长期位次之，普通位在后。
// 缺省 2/1/7 时等价于旧分布（slot0,1 奖杯；slot2 长期；slot3~9 普通）。
func cafeSlotKinds(mix map[string]int) []string {
	kinds := make([]string, 0, 10)
	for _, spec := range []struct {
		kind  string
		count int
	}{
		{"trophy", mix["trophy"]},
		{"longterm", mix["longterm"]},
		{"normal", mix["normal"]},
	} {
		for i := 0; i < spec.count; i++ {
			kinds = append(kinds, spec.kind)
		}
	}
	return kinds
}

// cafeSlotKind 默认槽位类别（不含后台占比），供缺省分布与测试使用。
func cafeSlotKind(slot int) string {
	kinds := cafeSlotKinds(cafeSlotMixDefaults())
	if slot < 0 || slot >= len(kinds) {
		return "normal"
	}
	return kinds[slot]
}

// cafePeriodDefaults 轮换时间缺省值（后台 cafe_slot_period 为空时兜底）。
// 长期位固定 24 小时、奖杯位固定 12 小时、普通/达标位逐槽 1~4 小时。
func cafePeriodDefaults() map[string]store.CafeSlotPeriod {
	return map[string]store.CafeSlotPeriod{
		"trophy":   {Kind: "trophy", MinSeconds: 43200, MaxSeconds: 43200},
		"longterm": {Kind: "longterm", MinSeconds: 86400, MaxSeconds: 86400},
		"normal":   {Kind: "normal", MinSeconds: 3600, MaxSeconds: 14400},
	}
}

// cafePeriodMap 用后台配置覆盖缺省周期；缺省行兜底，避免后台删行导致生成失败。
func cafePeriodMap(cfg store.CafeBountyConfig) map[string]store.CafeSlotPeriod {
	m := cafePeriodDefaults()
	for _, p := range cfg.Periods {
		if p.Kind == "" || p.MinSeconds <= 0 || p.MaxSeconds < p.MinSeconds {
			continue
		}
		m[p.Kind] = p
	}
	return m
}

// cafeQuantityDefaults 交单数量缺省值（后台 cafe_order_quantity 为空时兜底）。
func cafeQuantityDefaults() map[string]int64 {
	return map[string]int64{
		"common_normal":    5, // 普通鱼
		"common_qualified": 3, // 达标鱼
		"rare_normal":      3, // 稀有普通鱼
		"rare_qualified":   2, // 稀有达标鱼
	}
}

// cafeQuantityMap 用后台配置覆盖缺省交单数量。
func cafeQuantityMap(cfg store.CafeBountyConfig) map[string]int64 {
	m := cafeQuantityDefaults()
	for _, q := range cfg.Quantities {
		if q.QKey == "" || q.Quantity < 1 || q.Quantity > 1000 {
			continue
		}
		m[q.QKey] = q.Quantity
	}
	return m
}

// cafeQuantityKey 交单数量档位键：稀缺 × 重量档（普通 grade1 / 达标 grade2）。
func cafeQuantityKey(grade int64, isRare bool) string {
	g := "normal"
	if grade >= 2 {
		g = "qualified"
	}
	r := "common"
	if isRare {
		r = "rare"
	}
	return r + "_" + g
}

// cafeSlotSchedule 槽位的周期（秒）与相位；周期取后台配置，固定周期 min=max。
func cafeSlotSchedule(kind string, r *rand.Rand, periods map[string]store.CafeSlotPeriod) (period, phase int64) {
	p := periods[kind]
	period = p.MinSeconds
	if p.MaxSeconds > p.MinSeconds {
		period = p.MinSeconds + r.Int63n(p.MaxSeconds-p.MinSeconds+1)
	}
	if period <= 0 {
		period = 3600
	}
	phase = r.Int63n(period)
	return
}

// rarityFromCatches 按捕获权重排名算稀有度（1=最稀有），同权重取并列平均秩。
func rarityFromCatches(fishes []store.CafeLocFish) map[string]float64 {
	type entry struct {
		key     string
		catches int64
	}
	rows := make([]entry, 0, len(fishes))
	for _, f := range fishes {
		rows = append(rows, entry{f.FishKey, f.Catches})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].catches != rows[j].catches {
			return rows[i].catches < rows[j].catches
		}
		return rows[i].key < rows[j].key
	})
	out := make(map[string]float64, len(rows))
	if len(rows) <= 1 {
		for _, r := range rows {
			out[r.key] = 0.5
		}
		return out
	}
	denom := float64(len(rows) - 1)
	for i := 0; i < len(rows); {
		j := i + 1
		for j < len(rows) && rows[j].catches == rows[i].catches {
			j++
		}
		avgRank := float64(i+j-1) / 2
		out[rows[i].key] = 1 - avgRank/denom
		for k := i + 1; k < j; k++ {
			out[rows[k].key] = out[rows[i].key]
		}
		i = j
	}
	return out
}

// bountyInBand 在区间内按 t∈[0,1] 插值（金币分）。
func bountyInBand(lo, hi int64, t float64) int64 {
	if math.IsNaN(t) || t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return lo + int64(math.Round(t*float64(hi-lo)))
}

// rarityT rarity 在本格所属段内的归一化位置。
func rarityT(rarity float64, isRare bool) float64 {
	if isRare {
		return (rarity - cafeRareRarity) / (1 - cafeRareRarity)
	}
	return rarity / cafeRareRarity
}

// giantSubsidyFor 依据蓝冠线落档取补贴，稀有度决定区间内位置；巨物至少取中档。
func giantSubsidyFor(blueG int64, subsidies []store.CafeSubsidy, rarity float64) int64 {
	if blueG <= 0 {
		return 0
	}
	for _, sub := range subsidies { // 已按 min_blue_g 降序
		if blueG < sub.MinBlueG {
			continue
		}
		t := rarityT(rarity, true)
		if t < 0.3 {
			t = 0.3
		}
		return bountyInBand(sub.LoCents, sub.HiCents, t)
	}
	return 0
}

type poolFish struct {
	store.CafeLocFish
	rarity float64
	isRare bool
	isGiant bool
}

// buildPoolForLevel 为单个水域生成候选池行。
func buildPoolForLevel(ctx context.Context, db *store.DB, level, loc string, cfg store.CafeBountyConfig) ([]store.CafeBountyPoolRow, error) {
	fishes, err := db.CafeLocFishes(ctx, loc)
	if err != nil {
		return nil, err
	}
	// 白名单取自静态文件（rare/giant 分类），后台不再维护。
	whitelist := cafeWhitelistMap()
	// 黑名单按水域排除：命中的鱼种不进入本水域候选池。
	blacklist, err := db.CafeBlacklistByLevel(ctx, level)
	if err != nil {
		return nil, err
	}
	blocked := make(map[string]bool, len(blacklist))
	for _, b := range blacklist {
		blocked[b.FishKey] = true
	}
	// 必须有完整三线才能作为候选（档位线即重量要求）。
	usable := make([]poolFish, 0, len(fishes))
	rarityMap := rarityFromCatches(fishes)
	// 用户补丁：鱼种档位线兜底。老玩家库里沃尔霍夫河(level_005_volhov)、
	// 北顿涅茨河(level_013_sev_don)部分鱼缺 fish_grade_lines，原逻辑整条剔除，
	// 若整图可用鱼为零则 buildPoolForLevel 报错、RegenerateCafeBountyPool 整体中断，
	// 这些水域就永远没有候选池、咖啡厅没有订单。
	// 兜底：缺线的鱼按同图其它鱼线的中位数补齐；整图无参考线才用全局默认。
	type gradeLine struct{ q, t, b int64 }
	lines := make([]gradeLine, 0, len(fishes))
	for _, f := range fishes {
		if f.QualifiedG > 0 && f.TrophyG > 0 && f.BlueG > 0 {
			lines = append(lines, gradeLine{f.QualifiedG, f.TrophyG, f.BlueG})
		}
	}
	fallback := gradeLine{500, 3000, 5000} // 全局默认：合格/奖杯/蓝冠(g)
	if len(lines) > 0 {
		sort.Slice(lines, func(i, j int) bool { return lines[i].q < lines[j].q })
		fallback = lines[len(lines)/2]
	}
	for _, f := range fishes {
		if blocked[f.FishKey] {
			continue
		}
		q, t, b := f.QualifiedG, f.TrophyG, f.BlueG
		if q <= 0 || t <= 0 || b <= 0 {
			q, t, b = fallback.q, fallback.t, fallback.b
		}
		cat := whitelist[f.FishKey].Category
		pf := poolFish{CafeLocFish: f, rarity: rarityMap[f.FishKey]}
		pf.QualifiedG, pf.TrophyG, pf.BlueG = q, t, b
		pf.isGiant = cat == "giant"
		pf.isRare = cat == "rare" || pf.isGiant || pf.rarity >= cafeRareRarity
		usable = append(usable, pf)
	}
	if len(usable) == 0 {
		return nil, fmt.Errorf("水域 %s(%s) 无可用候选鱼种（含黑名单过滤）", level, loc)
	}

	bands := make(map[string]store.CafeBand, len(cfg.Bands))
	for _, b := range cfg.Bands {
		bands[b.Key] = b
	}

	// 分组池：巨物/稀有鱼进长期位；奖杯位排除巨物；普通位排除白名单鱼。
	var longPool, trophyPool, normalPool []poolFish
	for _, pf := range usable {
		cat := whitelist[pf.FishKey].Category
		if pf.isGiant || cat == "rare" {
			longPool = append(longPool, pf)
		}
		if !pf.isGiant {
			trophyPool = append(trophyPool, pf)
		}
		if cat != "giant" && cat != "rare" {
			normalPool = append(normalPool, pf)
		}
	}
	if len(longPool) == 0 {
		// 方案 a：无稀有/巨物白名单的水域，长期位用本图蓝冠线最高的普通鱼（本图巨物）。
		best := usable[0]
		for _, pf := range usable[1:] {
			if pf.BlueG > best.BlueG {
				best = pf
			}
		}
		longPool = []poolFish{best}
	}
	if len(normalPool) == 0 {
		normalPool = usable
	}
	if len(trophyPool) == 0 {
		trophyPool = usable
	}

	periods := cafePeriodMap(cfg)
	quantities := cafeQuantityMap(cfg)
	slotKinds := cafeSlotKinds(cafeSlotMixMap(cfg))
	rows := make([]store.CafeBountyPoolRow, 0, len(slotKinds)*cafePoolVariants)
	for slot, kind := range slotKinds {
		pool := normalPool
		switch kind {
		case "trophy":
			pool = trophyPool
		case "longterm":
			pool = longPool
		}
		r := cafeRandom(fmt.Sprintf("cafe-pool/%s/%d", level, slot))
		period, phase := cafeSlotSchedule(kind, r, periods)
		for v := 0; v < cafePoolVariants; v++ {
			pf := pool[r.Intn(len(pool))]
			weightG, grade := pf.QualifiedG, int64(2)
			qty := int64(1) // 奖杯位/长期位仍为单条大鱼
			switch kind {
			case "trophy":
				weightG, grade = pf.TrophyG, 3
			case "longterm":
				weightG, grade = pf.BlueG, 4
			default:
				// 7 位普通/达标位：偶数 variant 走普通档（grade1，重量取达标线一半），
				// 奇数 variant 走达标档（grade2，重量取达标线）；交单数量按稀缺×档位取配置。
				if v%2 == 0 {
					grade = 1
					weightG = pf.QualifiedG / 2
					if weightG < 1 {
						weightG = 1
					}
				}
				qty = quantities[cafeQuantityKey(grade, pf.isRare)]
				if qty < 1 {
					qty = 1
				}
			}
			band := bands[cafeBountyBandKey(kind, pf.isRare)]
			gold := bountyInBand(band.LoCents, band.HiCents, rarityT(pf.rarity, pf.isRare))
			var subsidy int64
			if pf.isGiant && kind == "longterm" {
				subsidy = giantSubsidyFor(pf.BlueG, cfg.Subsidies, pf.rarity)
			}
			gold += subsidy
			if gold > cafeBountyCapCents {
				gold = cafeBountyCapCents
			}
			rows = append(rows, store.CafeBountyPoolRow{
				Level: level, Slot: slot, Variant: v, Species: pf.FishKey, NameZH: pf.NameZH,
				WeightG: weightG, Grade: grade, Rarity: pf.rarity, IsRare: pf.isRare, IsGiant: pf.isGiant,
				GoldCents: gold, SubsidyCents: subsidy, PeriodSeconds: period, PhaseSeconds: phase,
				Quantity: qty,
			})
		}
	}
	return rows, nil
}

// CafeGoldPricingVersion 从候选池版本独立出来：候选池订单用 v5，避免与 v4 旧单混用。
const cafePoolPricingVersion = 5

// cafeTemplatesFromPool 从预生成候选池按现实时间周期选候选，转成订单模板。
// 每个槽位有多条 variant，按当前周期取模选择；同一周期内稳定，刷新不重置。
func (s *Server) cafeTemplatesFromPool(ctx context.Context, level string, now int64) ([]store.CafeOrderTemplate, error) {
	rows, err := s.DB.CafeBountyPoolByLevel(ctx, level)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil // 池未生成，调用方回退或跳过
	}
	bySlot := make(map[int][]store.CafeBountyPoolRow)
	for _, r := range rows {
		bySlot[r.Slot] = append(bySlot[r.Slot], r)
	}
	slots := make([]int, 0, len(bySlot))
	for slot := range bySlot {
		slots = append(slots, slot)
	}
	sort.Ints(slots)
	out := make([]store.CafeOrderTemplate, 0, len(slots))
	for _, slot := range slots {
		cands := bySlot[slot]
		if len(cands) == 0 {
			continue
		}
		// 槽位周期/相位在生成时固定写入，同一槽位各 variant 一致，取首条即可。
		period, phase := cands[0].PeriodSeconds, cands[0].PhaseSeconds
		cycle := int64(0)
		if now >= phase {
			cycle = (now - phase) / period
		}
		idx := int(cycle % int64(len(cands)))
		cand := cands[idx]
		qty := cand.Quantity
		if qty < 1 {
			qty = 1
		}
		out = append(out, store.CafeOrderTemplate{
			Species: cand.Species, MinWeightG: cand.WeightG, Quantity: qty,
			RewardCents: cand.GoldCents, OfferSize: cand.Grade,
			PeriodSeconds: period, PhaseSeconds: phase, PricingVersion: cafePoolPricingVersion,
		})
	}
	return out, nil
}

// RegenerateCafeBountyPool 重建全部水域候选池，返回各水域候选行数。
func (s *Server) RegenerateCafeBountyPool(ctx context.Context) (map[string]int, error) {
	cfg, err := s.DB.CafeBountyConfig(ctx)
	if err != nil {
		return nil, err
	}
	aliases, err := s.DB.CafeBountyLevels(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(aliases))
	var firstErr error
	for _, level := range aliases {
		loc := s.DB.ResolveStatLoc(ctx, level)
		rows, err := buildPoolForLevel(ctx, s.DB, level, loc, cfg)
		if err != nil {
			// 用户补丁：单水域生成失败不中断整体。原逻辑任一图报错就整体 return，
			// 排在出错图之后的沃尔霍夫河/北顿涅茨河就永远没池、没咖啡厅订单。
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := s.DB.CafeBountyPoolReplace(ctx, level, rows); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		counts[level] = len(rows)
	}
	// 单水域失败不阻断其它水域；错误仅记录，供调用方统计已完成的水域。
	if firstErr != nil && s != nil && s.log != nil {
		s.log.Warn("咖啡厅候选池部分水域重建失败", "err", firstErr, "ok_waters", len(counts))
	}
	return counts, nil
}
