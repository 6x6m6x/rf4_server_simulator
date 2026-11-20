package gameserver

import (
	"context"
	"testing"

)

// TestWorkshopHidesReelBodyAsDevice：用户补丁——改装非原件后出现的"装置"栏
// （cat=6 渔轮本体行，GUID=轮子自身）只在 21/1/21/2 展示层隐藏，
// 不影响 21/4 本体修理/换件功能与零件替代品。
func TestWorkshopHidesReelBodyAsDevice(t *testing.T) {
	srv, _ := newRigTestServer(t)
	ctx := context.Background()

	// 1. 隐藏判定精确：cat6 与摇柄34隐藏，真实零件品类不隐藏。
	if !workshopHiddenInUI(uint32(gearCatReel)) || !workshopHiddenInUI(34) {
		t.Fatal("渔轮本体(6)与摇柄(34)应在展示层隐藏")
	}
	for _, cat := range []uint32{4, 5, 7, 8, 9, 10, 52, 63, 81} {
		if workshopHiddenInUI(cat) {
			t.Fatalf("真实零件品类 %d 不应被隐藏", cat)
		}
	}

	// 2. 造一只轮子，本体行条件成立时 repairPartsOf 仍返回 cat6 行（21/4 功能保留）。
	reel, err := srv.CreateItem(ctx, 1000001, "elcon_5999", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.DB.SetItemDurability(ctx, reel.GUID, 0); err != nil {
		t.Fatal(err)
	}
	reel = mustItem(t, srv.DB, reel.GUID)
	parts := srv.repairPartsOf(ctx, reel)
	hasBody := false
	for _, p := range parts {
		if p.Cat == uint32(gearCatReel) {
			hasBody = true
		}
	}
	if !hasBody {
		t.Fatal("repairPartsOf 应仍能产生本体行（供 21/4 下单），不能从数据源删除")
	}

	// 3. 真实零件替代品仍非空（没关掉"选择替代品"功能）。
	found := false
	for _, p := range parts {
		if p.Cat == 8 {
			found = true
			if len(srv.partAlternatives(ctx, reel.ItemID, p)) == 0 {
				t.Fatal("线杯零件应仍有替代品")
			}
		}
	}
	if !found {
		t.Fatal("轮子缺少线杯零件，夹具失效")
	}

}
