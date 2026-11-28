package gameserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 管理后台「玩家 → 概况」里按需发放奖赏面板（3/16）的奖杯。
//
// 为什么不能直接用后台的「发物品」：那条路走 grantItemUnits → pushItemUpdate，
// 容器投影是背包/房屋，客户端把物品挂进背包组而不是奖赏分组（f4=14），
// 奖赏面板 3/16 读不到；而且里程碑类奖座还依赖 kv 幂等键判定"已发过"。
// 本入口按官方发放同型：prepareNewItem（house）+ kv 幂等键 + awardItemTree（f4=14）
// + pushAwardUpdate（4/22），玩家无需重登即可在奖赏面板看到。

type adminRewardTrophy struct {
	ItemID string `json:"item_id"`
	Name   string `json:"name"`
	Group  string `json:"group"`
}

// adminRewardTrophyCatalog 返回奖赏面板可发放的奖杯清单（分组、item_id、中文名）。
// 覆盖截图里的稀有鱼/超级稀有鱼奖座、水域奖杯、资格杯、周年/新年纪念杯，
// 以及其它 category=72 的奖座（去重，按 item_id 稳定排序）。
func (s *Server) adminRewardTrophyCatalog(ctx context.Context) []adminRewardTrophy {
	if s == nil || s.DB == nil {
		return nil
	}
	type defRow struct {
		ItemID string
		Name   string
	}
	rows, err := s.DB.SQL.QueryContext(ctx, `
		SELECT d.item_id, COALESCE(NULLIF(c.name_zh,''), NULLIF(c.name,''), d.item_id)
		FROM item_def d LEFT JOIN item_catalog c ON c.item_id=d.item_id
		WHERE d.category=72 ORDER BY d.item_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]adminRewardTrophy, 0, 128)
	for rows.Next() {
		var d defRow
		if err := rows.Scan(&d.ItemID, &d.Name); err != nil {
			continue
		}
		out = append(out, adminRewardTrophy{ItemID: d.ItemID, Name: d.Name, Group: rewardTrophyGroup(d.ItemID)})
	}
	return out
}

// rewardTrophyGroup 按 item_id 前缀给奖杯分组，供前端分组展示。
func rewardTrophyGroup(itemID string) string {
	switch {
	case strings.HasPrefix(itemID, "statuette_fisherman_"):
		return "稀有鱼奖座"
	case strings.HasPrefix(itemID, "statuette_supertrophy_"):
		return "超级稀有鱼奖座"
	case strings.HasPrefix(itemID, "trophy_level_"):
		return "水域奖杯"
	case strings.HasPrefix(itemID, "qual_"):
		return "资格杯"
	case strings.HasPrefix(itemID, "reward_bday") || strings.HasPrefix(itemID, "ny_20"), strings.HasPrefix(itemID, "statuette_6years"), strings.HasPrefix(itemID, "statuette_mednoe"), strings.HasPrefix(itemID, "statuette_wf_port"):
		return "周年与纪念"
	case strings.HasPrefix(itemID, "statuette_cards_"):
		return "集卡奖座"
	case strings.HasPrefix(itemID, "ft_"), strings.HasPrefix(itemID, "ann_rec_"), strings.HasPrefix(itemID, "weekly_"):
		return "赛事与周常"
	default:
		return "其它奖座"
	}
}

// adminGrantRewardTrophy 发放一枚奖赏奖杯到玩家的奖赏分组（3/16 可见）。
// 返回 true=本次新发；已拥有或已发过返回 false。
func (s *Server) adminGrantRewardTrophy(ctx context.Context, uid int64, itemID string) bool {
	if s == nil || s.DB == nil || uid <= 0 || itemID == "" {
		return false
	}
	// 已拥有就不重复发（奖杯是纪念品，卖掉也不重发）。
	var owned int64
	if err := s.DB.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, itemID).Scan(&owned); err != nil {
		return false
	}
	if owned > 0 {
		return false
	}
	def, err := s.DB.GetItemDefByItemID(ctx, itemID)
	if err != nil || def == nil || def.Category != 72 {
		return false // 只放 category=72 奖赏类，防止误发普通物品
	}
	it, err := s.prepareNewItem(ctx, uid, itemID, "house", 1, "")
	if err != nil {
		s.log.Warn("管理发放奖杯：构造失败", "uid", uid, "item", itemID, "err", err)
		return false
	}
	tx, err := beginEconomyTx(ctx, s.DB)
	if err != nil {
		return false
	}
	defer func() { _ = tx.Rollback() }()
	// 幂等键：与官方各奖杯族的键空间隔离，避免误触发官方巡检判定。
	key := fmt.Sprintf("admin-reward-trophy:%d:%s", uid, itemID)
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO kv(k,v) VALUES(?,?)`, key, strconv.FormatInt(time.Now().Unix(), 10))
	if err != nil {
		return false
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false
	}
	row, _, err := grantPreparedItemTx(ctx, tx, it, false)
	if err != nil {
		s.log.Warn("管理发放奖杯：入包失败", "uid", uid, "item", itemID, "err", err)
		return false
	}
	if err := tx.Commit(); err != nil {
		return false
	}
	// 推送 f4=14 奖赏分组，面板即时刷新（普通 pushItemUpdate 投影不对，面板看不到）。
	s.pushAwardUpdate(uid, row)
	return true
}

// invalidTradingCardIDs 返回「静态库定义坏了」的卡牌 item_id：鱼种不在
// species_weight（无中文名/无档位，客户端显示俄文占位名）或没有出售价
// （obcephkkfhi<=0，玩家卖不掉=清不掉）。2026-11-28 实测静态库有 5 张：
// card_c_bream / card_d_labrax / card_epigon_t / card_regalecus_g / card_sea_brama。
// 一键发卡必须跳过它们，否则会给玩家塞进清不掉的未知卡。
func (s *Server) invalidTradingCardIDs(ctx context.Context) map[string]bool {
	bad := map[string]bool{}
	if s == nil || s.DB == nil {
		return bad
	}
	rows, err := s.DB.SQL.QueryContext(ctx, `
		SELECT d.item_id, d.attrs_json,
		       (SELECT COUNT(*) FROM species_weight w WHERE w.fish_key=json_extract(d.attrs_json,'$.okppoalfakf'))
		FROM item_def d WHERE d.category=78`)
	if err != nil {
		return bad
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		var fishRows int64
		if err := rows.Scan(&id, &raw, &fishRows); err != nil {
			continue
		}
		var attrs struct {
			Fish  string  `json:"okppoalfakf"`
			Price float64 `json:"obcephkkfhi"`
		}
		if json.Unmarshal([]byte(raw), &attrs) != nil || attrs.Fish == "" || fishRows == 0 || attrs.Price <= 0 {
			bad[id] = true
		}
	}
	return bad
}

// tradingCardDeckNeeds 统计每张卡牌被多少本卡册（category 82）需要。
// 真实静态库里 65 张卡被 2~4 本卡册共用（如 card_s_orfe 被 4 本册引用），
// 而一张卡实例同一时刻只能入一本册（trading_card_albums.go 的
// "卡片已在其他卡册中" / "该鱼种已入册"），所以发卡必须按最大需求数给足副本，
// 否则多本共享卡不可能同时集齐。
func (s *Server) tradingCardDeckNeeds(ctx context.Context) map[string]int {
	need := map[string]int{}
	if s == nil || s.DB == nil {
		return need
	}
	rows, err := s.DB.SQL.QueryContext(ctx, `SELECT attrs_json FROM item_def WHERE category=82`)
	if err != nil {
		return need
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var attrs struct {
			Cards []string `json:"cjmjfnheidh"`
		}
		if err := json.Unmarshal([]byte(raw), &attrs); err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, id := range attrs.Cards {
			if id == "" || seen[id] {
				continue // 同一册内重复引用只算一次
			}
			seen[id] = true
			need[id]++
		}
	}
	return need
}

// grantAllTradingCardsForAdmin 一键发放所有卡牌与卡册（category 78/82）。
// ★2026-11-28 改进：卡牌按「被多少本卡册需要」补足副本（最多 4 份），
// 而不是每种鱼只发 1 张——多本合集共用同一鱼种时，1 张卡无法满足全部册子。
// 卡册仍每种 1 本；已拥有数量达到需求的自动跳过，重复执行幂等。
// CreateItem 内部会调 initializeTradingItem 给卡牌填上鱼种数据，走的是官方同型路径。
func (s *Server) grantAllTradingCardsForAdmin(uid int64) (cards, decks int) {
	if s == nil || s.DB == nil || uid <= 0 {
		return 0, 0
	}
	ctx := s.dbCtx()
	rows, err := s.DB.SQL.QueryContext(ctx, `
		SELECT item_id, category FROM item_def WHERE category IN (78,82) ORDER BY category,item_id`)
	if err != nil {
		return 0, 0
	}
	type row struct{ id string; cat int64 }
	list := make([]row, 0, 300)
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.cat); err == nil {
			list = append(list, r)
		}
	}
	_ = rows.Close()
	need := s.tradingCardDeckNeeds(ctx)
	invalid := s.invalidTradingCardIDs(ctx)
	for _, r := range list {
		if r.cat == 78 && invalid[r.id] {
			s.log.Warn("管理发放卡牌：跳过定义损坏的卡", "uid", uid, "item", r.id)
			continue // 坏卡不发，避免塞进玩家清不掉的未知卡
		}
		target := 1
		if r.cat == 78 {
			if n := need[r.id]; n > target {
				target = n // 共享卡给足副本，能同时入多本卡册
			}
		}
		var have int64
		if err := s.DB.SQL.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, r.id).Scan(&have); err != nil {
			continue
		}
		for have < int64(target) {
			it, err := s.CreateItem(ctx, uid, r.id, "backpack", 1)
			if err != nil || it == nil {
				s.log.Warn("管理发放卡牌失败", "uid", uid, "item", r.id, "err", err)
				break
			}
			s.pushItemUpdate(uid, it)
			have++
			if r.cat == 82 {
				decks++
			} else {
				cards++
			}
		}
	}
	s.log.Info("管理指令：一键发放所有卡牌（按卡册需求补足副本）", "uid", uid, "cards", cards, "decks", decks)
	return cards, decks
}

// cleanupInvalidCardsForAdmin 清理指定玩家（uid<=0=全服）手里「定义损坏」的卡牌：
// 鱼种不在 species_weight（俄文占位名）或没有出售价（清不掉）的 category=78 卡。
// 直接删 item 行并推 4/21 删除事件，玩家无需重登；卡册内已入册的坏卡一并清理
// （DeleteItemCascade 会解引用）。返回删除数量。
func (s *Server) cleanupInvalidCardsForAdmin(uid int64) int {
	if s == nil || s.DB == nil {
		return 0
	}
	ctx := s.dbCtx()
	invalid := s.invalidTradingCardIDs(ctx)
	if len(invalid) == 0 {
		return 0
	}
	// 一次查询同时取 guid/item_id；item 表本身没有 category 列，
	// 用 item_id 是否在坏卡集合里判定即可（坏卡集合已限定 category=78）。
	var rows *sql.Rows
	var err error
	if uid > 0 {
		rows, err = s.DB.SQL.QueryContext(ctx, `SELECT guid,item_id FROM item WHERE uid=?`, uid)
	} else {
		rows, err = s.DB.SQL.QueryContext(ctx, `SELECT guid,item_id FROM item`)
	}
	if err != nil {
		return 0
	}
	guids := make([]string, 0, 64)
	for rows.Next() {
		var guid, itemID string
		if err := rows.Scan(&guid, &itemID); err != nil {
			continue
		}
		if invalid[itemID] {
			guids = append(guids, guid)
		}
	}
	_ = rows.Close()
	deleted := 0
	for _, guid := range guids {
		row, gerr := s.DB.GetItemByGUID(ctx, guid)
		if gerr != nil || row == nil {
			continue
		}
		if err := s.DB.DeleteItemCascade(ctx, guid); err != nil {
			s.log.Warn("清理坏卡失败", "guid", guid, "err", err)
			continue
		}
		deleted++
		for _, sess := range s.sessionsForUID(row.UID) {
			sess.Push4_21([]string{guid})
			sess.Push2_1()
		}
	}
	s.log.Info("管理指令：清理损坏卡牌", "uid", uid, "deleted", deleted)
	return deleted
}
