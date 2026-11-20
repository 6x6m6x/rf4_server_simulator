package store

import (
	"context"
	"sort"
)

// 工坊店（kind=2）货架补齐（用户补丁）：
// 上游把零件品类（7轴承/8线杯/9摩擦片/10轮油/34摇柄/52齿轮/63电机/81引擎）
// 定义为“不单独在商店售卖”，且工坊店没有任何 shop_item 行 → 18 家工坊店货架
// 全空，客户端工坊“原件库房”读到空货架就把全部部件显示成“库房缺货”。
//
// 本文件在代码层给工坊店补一份配件货架（不动 static.sqlite3）：
//   - 品类限定为工坊零件，摇柄(34)按官服行为排除；
//   - 品牌店只补同品牌件，普通店补全部；
//   - 价格优先 static item_price/shop_item，缺失按品类基准价（银分）；
//   - 库存统一 1000，保证客户端显示有货。
// 玩家仍走 22/2 银币购买把备件真正买进背包，符合官服“先持有再安装”。

var workshopShopCategories = map[int64]int64{
	7:  845,  // 轴承
	8:  900,  // 线杯
	9:  754,  // 摩擦片
	10: 50,   // 轮油
	52: 296,  // 齿轮
	63: 800,  // 电机
	81: 1200, // 引擎
}

// workshopShelfFor 给工坊店生成配件货架；非工坊店返回 nil。
func (d *DB) workshopShelfFor(ctx context.Context, def shopStoreDefinition) []*ShopPrice {
	if def.Kind != 2 {
		return nil
	}
	if err := d.loadShopSupplement(ctx); err != nil {
		return nil
	}
	out := make([]*ShopPrice, 0, 512)
	for id, m := range d.shopSupplement.metas {
		base, ok := workshopShopCategories[m.Category]
		if !ok {
			continue
		}
		if len(def.Brands) > 0 && !brandMatches(m.Brand, def.Brands) {
			continue
		}
		var silver, gold int64
		if p, exists := d.shopSupplement.prices[id]; exists && (p.Silver > 0 || p.Gold > 0) {
			silver, gold = p.Silver, p.Gold
		} else {
			silver = base
			gold = (base + 55) / 110
		}
		out = append(out, &ShopPrice{ItemID: id, Silver: silver, Gold: gold, Stock: 1000})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ItemID < out[j].ItemID })
	return out
}
