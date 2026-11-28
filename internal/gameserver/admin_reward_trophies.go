package gameserver

import (
	"context"
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

// grantAllTradingCardsForAdmin 一键发放所有卡牌与卡册（category 78/82）。
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
		if err := rows.Scan(&r.id, &r.cat); err == nil { list = append(list, r) }
	}
	_ = rows.Close()
	for _, r := range list {
		var have int64
		if err := s.DB.SQL.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, r.id).Scan(&have); err != nil || have > 0 {
			continue // 已拥有跳过，保证幂等
		}
		it, err := s.CreateItem(ctx, uid, r.id, "backpack", 1)
		if err != nil || it == nil {
			s.log.Warn("管理发放卡牌失败", "uid", uid, "item", r.id, "err", err)
			continue
		}
		s.pushItemUpdate(uid, it)
		if r.cat == 82 { decks++ } else { cards++ }
	}
	s.log.Info("管理指令：一键发放所有卡牌", "uid", uid, "cards", cards, "decks", decks)
	return cards, decks
}
