package gameserver

import (
	"testing"

	"rf4server/internal/logging"
)

func TestWorkshopRepairsReelBodyWhenAllPartsLookFull(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	ctx := t.Context()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateBalance(ctx, uid, 1000, 0, 0); err != nil {
		t.Fatal(err)
	}
	reel, err := srv.CreateItem(ctx, uid, "elcon_5999", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetItemDurability(ctx, reel.GUID, 0); err != nil {
		t.Fatal(err)
	}
	row := mustItem(t, db, reel.GUID)
	parts := srv.repairPartsOf(ctx, row)
	foundBody := false
	for _, part := range parts {
		if part.Cat == uint32(gearCatReel) && part.Cond == 0 {
			foundBody = true
			break
		}
	}
	if !foundBody {
		t.Fatal("本体耐久为 0 时工坊必须提供本体修理行")
	}

	// 用户补丁：本体行在 21/1/21/2 展示层隐藏（"装置"栏），但数据源与 21/4 下单仍保留。
	// 原断言通过 21/2 诊断取报价，隐藏后取不到——改从 repairPartsOf 的本体行核对完好度，
	// 报价由 21/4 下单扣款（下方）验证。
	bodyCond := float64(-1)
	for _, part := range parts {
		if part.Cat == uint32(gearCatReel) {
			bodyCond = part.Cond
			break
		}
	}
	if bodyCond != 0 {
		t.Fatalf("满磨损本体行完好度应为 0，实得 %v", bodyCond)
	}

	c := &Ctx{
		Sess: newProbeSess(srv), Main: 21, Sub: 3,
		Tree: snapshotWorkshopRequestPart(reel.GUID, uint32(gearCatReel), reel.ItemID, 0),
		Log:  logging.Discard(),
	}
	if body, ok := hWorkshopOrder(c); !ok || len(body) == 0 {
		t.Fatal("本体修理订单应成功")
	}
	fresh := mustItem(t, db, reel.GUID)
	if fresh.Durability != 1 {
		t.Fatalf("本体修理后耐久应为 1，实得 %v", fresh.Durability)
	}
	profile, err := db.GetProfile(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Silver != 500 {
		t.Fatalf("满磨损本体修理应扣 500 银，余额=%d", profile.Silver)
	}
}
