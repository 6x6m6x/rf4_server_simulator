package gameserver

import (
	"context"
	"testing"
)

// TestWorkshopNoReelNamedDeviceOption：工坊改装页不得多出"以轮子型号命名的装置选项"。
// 根因：本体行(cat=6)的 partAlternatives 曾先 addJob(def.ItemID) 把整只轮子塞进
// 自己的替代品列表，客户端渲染成名为轮子型号的"装置"。修复后本体行 f5 为空。
func TestWorkshopNoReelNamedDeviceOption(t *testing.T) {
	srv, _ := newRigTestServer(t)
	ctx := context.Background()
	const reelID = "spin_5820"

	reel, err := srv.CreateItem(ctx, 1000001, reelID, "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	parts := srv.repairPartsOf(ctx, reel)

	// 造一个与 repairPartsOf 本体行同构的条目（cat=6，NID=轮子定义），
	// 直接查它的替代品——修复后应为空，否则客户端会渲染出轮子名的"装置"。
	reelPart := repairPart{Cat: uint32(gearCatReel), GUID: reel.GUID, NID: uint32(reel.NumericID), Cond: 0.8}
	alts := srv.partAlternatives(ctx, reelID, reelPart)
	if len(alts) != 0 {
		t.Fatalf("本体行不应有任何替代工单（否则客户端显示轮子名的装置选项），实得 %d 条", len(alts))
	}

	// 对照：真实零件仍有替代品，确保没把整条替代品机制关掉。
	found := false
	for _, part := range parts {
		if part.Cat == 8 {
			found = true
			if got := srv.partAlternatives(ctx, reelID, part); len(got) == 0 {
				t.Fatal("线杯零件应仍有替代工单，不能连正常替代品一起关掉")
			}
		}
	}
	if !found {
		t.Fatal("轮子缺少线杯零件，测试夹具失效")
	}
}
