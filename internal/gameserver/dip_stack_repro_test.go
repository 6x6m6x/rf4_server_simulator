package gameserver

import (
	"context"
	"database/sql"
	"testing"

	"rf4server/internal/store"
)

// 复现：cat58 小药买两份应合堆为 200，不应开两个格子。
func TestDipStackMergeRepro(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	def, err := db.GetItemDefByItemID(ctx, "dip_hvz_b")
	if err != nil || def == nil {
		t.Skip("静态库缺 dip_hvz_b: ", err)
	}
	const uid = int64(1000099)
	if _, err := db.EnsureProfile(ctx, uid); err != nil { t.Fatal(err) }
	mergeable := craftOutputMerges(def)
	// 用户补丁断言：cat58 不在退货白名单 → 商店购买必须可合堆。
	if storeReturnCategoryAllowed(def.Category) {
		t.Fatalf("cat58 不应在退货白名单（否则商店购买不合堆，出现两个100）")
	}
	t.Logf("category=%d mergeable=%v qtyField(meta)=%d qtyField(stack)=%d", def.Category, mergeable,
		itemQtyFieldIdx("bpmmimcmpof"), stackQtyFieldIdx("bpmmimcmpof"))
	for i := 0; i < 2; i++ {
		prepared, err := srv.prepareNewItem(ctx, uid, def.ItemID, "backpack", 1, "")
		if err != nil { t.Fatal(err) }
		fields, _ := FieldsFromJSON(prepared.FieldsJSON)
		t.Logf("buy%d prepared.Qty=%d meta=%d stack=%d", i, prepared.Qty,
			u64Of(fields[itemQtyFieldIdx("bpmmimcmpof")].Num), u64Of(fields[stackQtyFieldIdx("bpmmimcmpof")].Num))
		tx, err := db.SQL.BeginTx(ctx, nil)
		if err != nil { t.Fatal(err) }
		row, merged, err := grantPreparedItemTx(ctx, tx, prepared, mergeable)
		if err != nil { t.Fatal(err) }
		if err := tx.Commit(); err != nil { t.Fatal(err) }
		f2, _ := FieldsFromJSON(row.FieldsJSON)
		t.Logf("buy%d merged=%v row.Qty=%d meta=%d stack=%d", i, merged, row.Qty,
			u64Of(f2[itemQtyFieldIdx("bpmmimcmpof")].Num), u64Of(f2[stackQtyFieldIdx("bpmmimcmpof")].Num))
	}
	var rows int
	if err := db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, def.ItemID).Scan(&rows); err != nil && err != sql.ErrNoRows { t.Fatal(err) }
	t.Logf("最终格子数=%d 期望1", rows)
	if rows != 1 { t.Fatalf("应合堆为1格，实际%d格", rows) }
	var total int64
	_ = db.SQL.QueryRowContext(ctx, `SELECT COALESCE(SUM(qty),0) FROM item WHERE uid=? AND item_id=?`, uid, def.ItemID).Scan(&total)
	t.Logf("总数量=%d 期望200", total)
	if total != 200 { t.Fatalf("应合堆200份，实际%d", total) }
}

// cat58 小药：咬钩上鱼消耗 1 份，抛竿不消耗。
func TestDipConsumeOnCatchOnly(t *testing.T) {
	def := &store.ItemDef{Category: 58, ItemID: "dip_hvz_b", AttrsJSON: `{"ljnnpokapig":100}`}
	if consumed, wear := baitUseRule(def, true); consumed || wear != 0 {
		t.Fatalf("抛竿不应消耗小药: consumed=%v wear=%v", consumed, wear)
	}
	if consumed, wear := baitUseRule(def, false); !consumed || wear != 0 {
		t.Fatalf("上鱼应消耗小药1份: consumed=%v wear=%v", consumed, wear)
	}
}
