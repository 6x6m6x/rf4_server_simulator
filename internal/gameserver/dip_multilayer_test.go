package gameserver

import (
	"context"
	"strings"
	"testing"

)

// 小药挂在水底钓组（多层中间节点）下时，上鱼确认仍应扣 1 份。
// 回归：旧逻辑要求 row.RigGUID == rod 精确相等，多层挂载被误跳过。
func TestDipConsumeMultiLayerMount(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	def, err := db.GetItemDefByItemID(ctx, "dip_hvz_b")
	if err != nil || def == nil { t.Skip("静态库缺 dip_hvz_b: ", err) }
	rod := baitTestRod(t, srv)
	// 中间层：把一个小药先挂到竿上，再用它作为"父节点"挂第二个小药（模拟多层链）
	_, mid := baitTestMount(t, srv, rod, def.ItemID, 50, 0)
	if mid == "" { t.Fatal("中间层挂载失败") }
	midRow, err := db.GetItemByGUID(ctx, mid)
	if err != nil || midRow == nil { t.Fatal(err) }
	if midRow.RigGUID != rod { t.Fatalf("中间层RigGUID应=rod: %s vs %s", midRow.RigGUID, rod) }
	// 第二个小药挂到中间层（RigGUID=mid，不是 rod）
	item, err := srv.CreateItem(ctx, 1000001, def.ItemID, "backpack", 50)
	if err != nil { t.Fatal(err) }
	srv.setStackCount(ctx, item, 50)
	r := srv.rigSetComponent(ctx, 1000001, mid, 0, item.GUID, 1, rigSetOpts{ParentType: familyRod, EnforceCategory: true, SplitStack: true})
	if r.Rejected || r.MountedGUID == "" { t.Skip("多层挂载被拒: ", r) }
	leaf := r.MountedGUID
	leafRow, _ := db.GetItemByGUID(ctx, leaf)
	if leafRow == nil || leafRow.RigGUID != mid { t.Skipf("未形成多层链: %+v", leafRow) }
	snapshot, known := srv.biteBaitSnapshot(ctx, rod)
	if !known { t.Fatal("快照未知") }
	found := false
	for _, g := range strings.Fields(snapshot) { if g == leaf { found = true } }
	if !found { t.Fatalf("快照缺多层小药 %s: %q", leaf, snapshot) }
	change, err := srv.consumeBaitEvent(ctx, 1000001, rod, "dip-multilayer", []string{leaf}, false)
	if err != nil { t.Fatal(err) }
	if len(change.Removed) == 0 && len(change.Updated) == 0 {
		t.Fatalf("多层小药未扣减: %+v", change)
	}
}
