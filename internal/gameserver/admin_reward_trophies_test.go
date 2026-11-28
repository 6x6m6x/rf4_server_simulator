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

// TestAdminGrantAllCardsSharedDeckCopies：多本卡册共用同一鱼种时，必须发足副本，
// 否则一张卡只能入一本册，其余合集永远集不齐。
// TestAdminGrantAllCardsSkipsInvalid：一键发卡不得发放定义损坏的卡
// （鱼种缺失显示俄文名、售价 0 清不掉），存量坏卡可被清理且不影响正常卡。
func TestAdminGrantAllCardsSkipsInvalid(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	ctx := context.Background()
	const uid = int64(1000006)
	if _, err := db.EnsureProfile(ctx, uid); err != nil { t.Fatal(err) }
	bad := srv.invalidTradingCardIDs(ctx)
	const knownBad = "card_c_bream"
	if !bad[knownBad] {
		t.Fatalf("静态库应把 %s 判为损坏卡", knownBad)
	}
	srv.grantAllTradingCardsForAdmin(uid)
	var badCount int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, knownBad).Scan(&badCount); err != nil {
		t.Fatal(err)
	}
	if badCount != 0 { t.Fatalf("一键发卡不应发放坏卡 %s，实得 %d 张", knownBad, badCount) }

	// 手工塞一张坏卡模拟存量玩家（绕过 CreateItem 的鱼种校验），再执行清理。
	var numericID int64
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT numeric_id FROM item_def WHERE item_id=?`, knownBad).Scan(&numericID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx,
		`INSERT INTO item(guid,uid,item_id,numeric_id,wire_class,type_id,container,slot,qty) VALUES(?,?,?,?,?,?,?,?,?)`,
		"badcard000000000000000000000001", uid, knownBad, numericID, "dapkdcghfmg", 11301, "native:18", 0, 1); err != nil {
		t.Fatal(err)
	}
	if n := srv.cleanupInvalidCardsForAdmin(uid); n < 1 {
		t.Fatalf("清理应至少删除 1 张坏卡，实得 %d", n)
	}
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, knownBad).Scan(&badCount); err != nil {
		t.Fatal(err)
	}
	if badCount != 0 { t.Fatalf("清理后坏卡应清零，实得 %d", badCount) }
	// 正常卡不受影响。
	var goodCount int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item WHERE uid=? AND item_id='card_s_orfe'`, uid).Scan(&goodCount); err != nil || goodCount == 0 {
		t.Fatalf("正常卡应保留: count=%d err=%v", goodCount, err)
	}
}

func TestAdminGrantAllCardsSharedDeckCopies(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	ctx := context.Background()
	const uid = int64(1000005)
	if _, err := db.EnsureProfile(ctx, uid); err != nil { t.Fatal(err) }
	need := srv.tradingCardDeckNeeds(ctx)
	// 找一张被两本以上卡册引用的共享卡；静态库里 card_s_orfe 最多（4 本）。
	shared := ""
	maxNeed := 1
	for id, n := range need {
		if n > maxNeed { shared, maxNeed = id, n }
	}
	if shared == "" { t.Fatal("静态库应存在多卡册共享的卡牌") }
	// 先手动发一张，验证一键发放会把副本补到需求数而非跳过。
	if _, err := srv.CreateItem(ctx, uid, shared, "backpack", 1); err != nil { t.Fatal(err) }
	srv.grantAllTradingCardsForAdmin(uid)
	var have int
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, shared).Scan(&have); err != nil {
		t.Fatal(err)
	}
	if have < maxNeed {
		t.Fatalf("共享卡 %s 只有 %d 张，少于 %d 本卡册的需求", shared, have, maxNeed)
	}
	c2, d2 := srv.grantAllTradingCardsForAdmin(uid)
	if c2 != 0 || d2 != 0 { t.Fatalf("补足后重复执行应全部跳过: %d/%d", c2, d2) }
}
