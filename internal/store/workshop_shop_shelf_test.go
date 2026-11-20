package store

import (
	"context"
	"path/filepath"
	"testing"
)

// TestWorkshopShopShelfSupplement：工坊店(kind=2)上游货架全空导致客户端
// "原件库房"显示缺货。本补丁在代码层补配件货架，不动静态库。
func TestWorkshopShopShelfSupplement(t *testing.T) {
	db, err := OpenWithStatic(filepath.Join(t.TempDir(), "workshop-shop.db"),
		filepath.Join("..", "..", "server_data", "static.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	// 工坊店：应有配件货架（摩擦片/轮油等），库存>0。
	res, err := db.effectiveShopShelf(ctx, "workshop_01")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Effective) < 50 {
		t.Fatalf("工坊店货架过少: %d", len(res.Effective))
	}
	cats := map[int64]int{}
	hasFriction, hasOil := false, false
	for _, p := range res.Effective {
		if p.Stock <= 0 {
			t.Fatalf("配件库存应>0: %s stock=%d", p.ItemID, p.Stock)
		}
		if p.Silver <= 0 && p.Gold <= 0 {
			t.Fatalf("配件应有有效价格: %s", p.ItemID)
		}
		m, ok := db.shopSupplement.metas[p.ItemID]
		if !ok {
			continue
		}
		cats[m.Category]++
		if m.Category == 9 {
			hasFriction = true
		}
		if m.Category == 10 {
			hasOil = true
		}
		if m.Category == 34 {
			t.Fatalf("摇柄(34)按官服不应上货架: %s", p.ItemID)
		}
	}
	if !hasFriction || !hasOil {
		t.Fatalf("工坊货架应含摩擦片与轮油: cats=%v", cats)
	}

	// 非工坊店（咖啡馆）不应被配件污染。
	cafe, err := db.effectiveShopShelf(ctx, "cafestore_18")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cafe.Effective {
		if m, ok := db.shopSupplement.metas[p.ItemID]; ok && workshopShopCategories[m.Category] > 0 {
			t.Fatalf("非工坊店不应上配件: %s", p.ItemID)
		}
	}
}
