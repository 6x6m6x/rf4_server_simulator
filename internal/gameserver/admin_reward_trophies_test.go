package gameserver

import (
	"context"
	"testing"
)

// TestAdminGrantRewardTrophyVisibleInPanel：按需发放的奖杯必须能在奖赏面板 3/16 读到
// （wire_class=enikeammfen / type_id=2015），重复发放幂等。
func TestAdminGrantRewardTrophyVisibleInPanel(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	ctx := context.Background()
	const uid = int64(1000003)
	if _, err := db.EnsureProfile(ctx, uid); err != nil { t.Fatal(err) }
	const itemID = "statuette_fisherman_15"
	if !srv.adminGrantRewardTrophy(ctx, uid, itemID) { t.Fatal("首次发放应成功") }
	// 面板读的就是 playerRewardItems 的过滤条件。
	rows, err := srv.playerRewardItems(ctx, uid)
	if err != nil { t.Fatal(err) }
	found := false
	for _, it := range rows {
		if it.ItemID == itemID {
			found = true
			if it.WireClass != "enikeammfen" || it.TypeID != 2015 {
				t.Fatalf("奖杯 wire 不对，面板不显示: %+v", it)
			}
		}
	}
	if !found { t.Fatal("奖杯未出现在奖赏面板数据源 3/16") }
	if srv.adminGrantRewardTrophy(ctx, uid, itemID) { t.Fatal("重复发放应幂等") }
}

// TestAdminGrantAllCards：一键发放所有卡牌/卡册，已拥有跳过，重复执行不重复发。
func TestAdminGrantAllCards(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	ctx := context.Background()
	const uid = int64(1000004)
	if _, err := db.EnsureProfile(ctx, uid); err != nil { t.Fatal(err) }
	cards, decks := srv.grantAllTradingCardsForAdmin(uid)
	if cards == 0 || decks == 0 { t.Fatalf("应发到卡牌与卡册: cards=%d decks=%d", cards, decks) }
	var n int
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM item WHERE uid=? AND item_id LIKE 'card_%'`, uid).Scan(&n); err != nil || n < cards {
		t.Fatalf("卡牌入库数 %d < %d err=%v", n, cards, err)
	}
	c2, d2 := srv.grantAllTradingCardsForAdmin(uid)
	if c2 != 0 || d2 != 0 { t.Fatalf("重复执行应全部跳过: %d/%d", c2, d2) }
}
