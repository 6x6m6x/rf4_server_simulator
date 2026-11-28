package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/store"
)

// seedCafePoolStatic 造最小可钓图数据：2 普通鱼 + 1 稀有鱼 + 1 巨物，均带完整三线。
func seedCafePoolStatic(t *testing.T, db *store.DB) {
	t.Helper()
	execs := []string{
		`INSERT OR REPLACE INTO loc_alias(alias,loc,note) VALUES('level_099_test','测试湖','test')`,
		`INSERT OR REPLACE INTO item_catalog(item_id,name, name_zh,kind) VALUES('common_fish','x','常见鱼','fish')`,
		`INSERT OR REPLACE INTO item_catalog(item_id,name, name_zh,kind) VALUES('rare_fish','x','稀有鱼','fish')`,
		`INSERT OR REPLACE INTO item_catalog(item_id,name, name_zh,kind) VALUES('giant_fish','x','巨物鱼','fish')`,
		`INSERT OR REPLACE INTO loc_fish(loc,fish_key,name_zh,catches) VALUES('测试湖','common_fish','常见鱼',50)`,
		`INSERT OR REPLACE INTO loc_fish(loc,fish_key,name_zh,catches) VALUES('测试湖','rare_fish','稀有鱼',1)`,
		`INSERT OR REPLACE INTO loc_fish(loc,fish_key,name_zh,catches) VALUES('测试湖','giant_fish','巨物鱼',3)`,
		`INSERT OR REPLACE INTO fish_grade_lines(fish_key,qualified_g,trophy_g,blue_trophy_g) VALUES('common_fish',500,2000,5000)`,
		`INSERT OR REPLACE INTO fish_grade_lines(fish_key,qualified_g,trophy_g,blue_trophy_g) VALUES('rare_fish',100,500,1000)`,
		`INSERT OR REPLACE INTO fish_grade_lines(fish_key,qualified_g,trophy_g,blue_trophy_g) VALUES('giant_fish',1000,50000,200000)`,
	}
	for _, q := range execs {
		if _, err := db.SQL.Exec(q); err != nil {
			t.Fatalf("seed 失败 %q: %v", q, err)
		}
	}
	// 白名单已固化到静态文件；测试注入等价分类（稀有鱼 + 巨物）。
	cafeWhitelistOverride = map[string]store.CafeFish{
		"rare_fish":  {FishKey: "rare_fish", Category: "rare", NameZH: "稀有鱼"},
		"giant_fish": {FishKey: "giant_fish", Category: "giant", NameZH: "巨物鱼"},
	}
}

// resetCafeWhitelist 在测试结束后恢复静态白名单。
func resetCafeWhitelist() { cafeWhitelistOverride = nil }

func TestCafeBountyPoolGeneration(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	defer db.Close()
	seedCafePoolStatic(t, db)
	defer resetCafeWhitelist()
	ctx := context.Background()

	counts, err := srv.RegenerateCafeBountyPool(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts["level_099_test"] != 10*cafePoolVariants {
		t.Fatalf("候选数 %d，期望 %d", counts["level_099_test"], 10*cafePoolVariants)
	}
	rows, err := db.CafeBountyPoolByLevel(ctx, "level_099_test")
	if err != nil {
		t.Fatal(err)
	}
	slots := map[int]bool{}
	for _, r := range rows {
		slots[r.Slot] = true
		if r.GoldCents <= 0 || r.WeightG <= 0 || r.Grade < 1 || r.Grade > 4 {
			t.Fatalf("非法候选: %+v", r)
		}
		if r.Quantity < 1 || r.Quantity > 1000 {
			t.Fatalf("交单数量越界: %+v", r)
		}
		// 奖杯位/长期位恒为 1 条；普通/达标位按档位配置（此处 1~5）。
		if r.Slot <= 2 && r.Quantity != 1 {
			t.Fatalf("奖杯/长期位应单条: %+v", r)
		}
		if r.Slot >= 3 && r.Quantity > 5 {
			t.Fatalf("普通/达标位数量超出默认上限: %+v", r)
		}
		// 巨物长期位必须带补贴且不超封顶。
		if r.IsGiant && r.Slot == 2 {
			if r.SubsidyCents <= 0 || r.GoldCents > cafeBountyCapCents {
				t.Fatalf("巨物补贴异常: %+v", r)
			}
		}
	}
	if len(slots) != 10 {
		t.Fatalf("槽位数 %d", len(slots))
	}

	// 同一时刻生成稳定。
	a, err := srv.cafeTemplatesFromPool(ctx, "level_099_test", 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	b, err := srv.cafeTemplatesFromPool(ctx, "level_099_test", 1700000000)
	if err != nil || len(a) != 10 || len(a) != len(b) {
		t.Fatalf("订单生成不稳定: %v %v", a, b)
	}
	for i := range a {
		if a[i].Species != b[i].Species || a[i].RewardCents != b[i].RewardCents {
			t.Fatalf("同一周期订单不一致: %+v vs %+v", a[i], b[i])
		}
		if a[i].PricingVersion != cafePoolPricingVersion {
			t.Fatalf("定价版本错误: %d", a[i].PricingVersion)
		}
	}
}

func TestCafePoolUsesConfiguredQuantityAndPeriod(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	defer db.Close()
	seedCafePoolStatic(t, db)
	defer resetCafeWhitelist()
	ctx := context.Background()

	// 把四类交单数量与普通位周期改成非缺省值，断言候选池按配置落库。
	execs := []string{
		`UPDATE cafe_order_quantity SET quantity=6 WHERE qkey='common_normal'`,
		`UPDATE cafe_order_quantity SET quantity=4 WHERE qkey='common_qualified'`,
		`UPDATE cafe_order_quantity SET quantity=3 WHERE qkey='rare_normal'`,
		`UPDATE cafe_order_quantity SET quantity=2 WHERE qkey='rare_qualified'`,
		`UPDATE cafe_slot_period SET min_seconds=1800, max_seconds=1800 WHERE kind='normal'`,
		`UPDATE cafe_slot_period SET min_seconds=86400, max_seconds=86400 WHERE kind='longterm'`,
	}
	for _, q := range execs {
		if _, err := db.SQL.Exec(q); err != nil {
			t.Fatalf("配置更新失败 %q: %v", q, err)
		}
	}
	if _, err := srv.RegenerateCafeBountyPool(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := db.CafeBountyPoolByLevel(ctx, "level_099_test")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Slot >= 3 && r.PeriodSeconds != 1800 {
			t.Fatalf("普通位周期未按配置: slot=%d period=%d", r.Slot, r.PeriodSeconds)
		}
		if r.Slot == 2 && r.PeriodSeconds != 86400 {
			t.Fatalf("长期位周期未按配置: %d", r.PeriodSeconds)
		}
		if r.Slot >= 3 {
			want := int64(3) // 稀有普通鱼
			if !r.IsRare && r.Grade == 1 {
				want = 6
			} else if !r.IsRare && r.Grade >= 2 {
				want = 4
			} else if r.IsRare && r.Grade >= 2 {
				want = 2
			}
			if r.Quantity != want {
				t.Fatalf("交单数量未按配置: slot=%d grade=%d rare=%v qty=%d want=%d",
					r.Slot, r.Grade, r.IsRare, r.Quantity, want)
			}
		}
	}

	// 订单模板应携带候选行的数量。
	tpls, err := srv.cafeTemplatesFromPool(ctx, "level_099_test", 1700000000)
	if err != nil {
		t.Fatal(err)
	}
	for i, tpl := range tpls {
		if tpl.Quantity < 1 {
			t.Fatalf("模板数量非法: slot=%d qty=%d", i, tpl.Quantity)
		}
	}
}

func TestGiantSubsidyAndCap(t *testing.T) {
	subs := []store.CafeSubsidy{
		{Grp: "g1", MinBlueG: 1000000, LoCents: 4000, HiCents: 5200},
		{Grp: "g2", MinBlueG: 100000, LoCents: 2000, HiCents: 4000},
		{Grp: "g3", MinBlueG: 0, LoCents: 1000, HiCents: 2000},
	}
	if got := giantSubsidyFor(2500000, subs, 0.9); got < 4000 || got > 5200 {
		t.Fatalf("姥鲨补贴 %d 越界", got)
	}
	if got := giantSubsidyFor(300000, subs, 0.9); got < 2000 || got > 4000 {
		t.Fatalf("波罗的海鲟补贴 %d 越界", got)
	}
	if got := giantSubsidyFor(14000, subs, 0.9); got < 1000 || got > 2000 {
		t.Fatalf("小体鲟补贴 %d 越界", got)
	}
	if got := giantSubsidyFor(0, subs, 0.9); got != 0 {
		t.Fatalf("无蓝冠线不应有补贴: %d", got)
	}
}

func TestSlotKindDistribution(t *testing.T) {
	want := []string{"trophy", "trophy", "longterm", "normal", "normal", "normal", "normal", "normal", "normal", "normal"}
	for slot, exp := range want {
		if got := cafeSlotKind(slot); got != exp {
			t.Fatalf("slot %d = %s, want %s", slot, got, exp)
		}
	}
}

func TestCafeSlotMixConfigurable(t *testing.T) {
	// 合法配比按奖杯/长期/普通顺序展开，总数恒为 10。
	mix := map[string]int{"trophy": 3, "longterm": 2, "normal": 5}
	kinds := cafeSlotKinds(mix)
	if len(kinds) != 10 {
		t.Fatalf("槽位总数 %d", len(kinds))
	}
	want := []string{"trophy", "trophy", "trophy", "longterm", "longterm",
		"normal", "normal", "normal", "normal", "normal"}
	for i, exp := range want {
		if kinds[i] != exp {
			t.Fatalf("slot %d = %s, want %s", i, kinds[i], exp)
		}
	}
	// 后台配置覆盖缺省；总和非法则回退缺省。
	cfg := store.CafeBountyConfig{SlotMix: []store.CafeSlotMix{
		{Kind: "trophy", Count: 3}, {Kind: "longterm", Count: 2}, {Kind: "normal", Count: 5},
	}}
	got := cafeSlotMixMap(cfg)
	if got["trophy"] != 3 || got["longterm"] != 2 || got["normal"] != 5 {
		t.Fatalf("配比覆盖失败: %+v", got)
	}
	bad := store.CafeBountyConfig{SlotMix: []store.CafeSlotMix{
		{Kind: "trophy", Count: 9}, {Kind: "longterm", Count: 1}, {Kind: "normal", Count: 5},
	}}
	fb := cafeSlotMixMap(bad)
	if fb["trophy"] != 2 || fb["longterm"] != 1 || fb["normal"] != 7 {
		t.Fatalf("非法配比应回退缺省: %+v", fb)
	}
}

func TestCafeBlacklistFiltersPool(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	defer db.Close()
	seedCafePoolStatic(t, db)
	defer resetCafeWhitelist()
	ctx := context.Background()

	// 把常见鱼加入本水域黑名单；重建后候选池不应再出现该鱼种。
	level := "level_099_test"
	if err := db.CafeBlacklistAdd(ctx, store.CafeBlacklist{Level: level, FishKey: "common_fish", NameZH: "常见鱼"}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.RegenerateCafeBountyPool(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := db.CafeBountyPoolByLevel(ctx, level)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Species == "common_fish" {
			t.Fatalf("黑名单鱼种仍出现在候选池: %+v", r)
		}
	}
	// 其它水域不受影响：黑名单按水域隔离。
	bl, err := db.CafeBlacklistByLevel(ctx, level)
	if err != nil || len(bl) != 1 || bl[0].FishKey != "common_fish" {
		t.Fatalf("黑名单读取异常: %+v %v", bl, err)
	}
	other, err := db.CafeBlacklistByLevel(ctx, "level_other")
	if err != nil || len(other) != 0 {
		t.Fatalf("黑名单未按水域隔离: %+v %v", other, err)
	}
}

func TestCafeSlotMixDrivesPoolRows(t *testing.T) {
	srv, db := newEconomyTxTestServer(t)
	defer db.Close()
	seedCafePoolStatic(t, db)
	defer resetCafeWhitelist()
	ctx := context.Background()

	if err := db.CafeSlotMixSetAll(ctx, map[string]int{"trophy": 3, "longterm": 2, "normal": 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.RegenerateCafeBountyPool(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := db.CafeBountyPoolByLevel(ctx, "level_099_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 10*cafePoolVariants {
		t.Fatalf("候选数 %d", len(rows))
	}
	byKind := map[string]int{"trophy": 0, "longterm": 0, "normal": 0}
	seen := map[int]bool{}
	for _, r := range rows {
		if seen[r.Slot] {
			continue
		}
		seen[r.Slot] = true
		kind := cafeSlotKinds(map[string]int{"trophy": 3, "longterm": 2, "normal": 5})[r.Slot]
		byKind[kind]++
	}
	if byKind["trophy"] != 3 || byKind["longterm"] != 2 || byKind["normal"] != 5 {
		t.Fatalf("池槽位分布未按配比: %+v", byKind)
	}
	// 订单模板槽位数也应随配比变为 10（总数不变），且结构稳定。
	tpls, err := srv.cafeTemplatesFromPool(ctx, "level_099_test", 1700000000)
	if err != nil || len(tpls) != 10 {
		t.Fatalf("模板数 %d, err=%v", len(tpls), err)
	}
	// 非法总和被拒。
	if err := db.CafeSlotMixSetAll(ctx, map[string]int{"trophy": 9, "longterm": 1, "normal": 5}); err == nil {
		t.Fatal("总和 15 的配比应被拒绝")
	}
}

func TestCafeFishSearchByChinese(t *testing.T) {
	_, db := newEconomyTxTestServer(t)
	defer db.Close()
	seedCafePoolStatic(t, db)
	defer resetCafeWhitelist()
	ctx := context.Background()
	// seed 已建常见鱼/稀有鱼/巨物鱼三行 loc_fish + item_catalog 中文名。
	// 中文名模糊匹配。
	items, err := db.CafeFishSearch(ctx, "常见", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 || items[0].FishKey != "common_fish" {
		t.Fatalf("中文搜索失败: %+v", items)
	}
	// fish_key 模糊匹配。
	items, err = db.CafeFishSearch(ctx, "giant_", 50)
	if err != nil || len(items) == 0 || items[0].FishKey != "giant_fish" {
		t.Fatalf("key 搜索失败: %+v %v", items, err)
	}
	// 精确中文名也可命中（item_catalog 的 name_zh 优先）。
	items, err = db.CafeFishSearch(ctx, "稀有鱼", 50)
	if err != nil || len(items) == 0 || items[0].NameZH != "稀有鱼" {
		t.Fatalf("item_catalog 中文名搜索失败: %+v %v", items, err)
	}
}

func TestCafeQuantityMatrixAndPeriods(t *testing.T) {
	// 缺省交单数量：普通鱼5 / 达标鱼3 / 稀有普通3 / 稀有达标2。
	q := cafeQuantityDefaults()
	cases := []struct {
		grade  int64
		rare   bool
		key    string
		want   int64
	}{
		{1, false, "common_normal", 5},
		{2, false, "common_qualified", 3},
		{1, true, "rare_normal", 3},
		{2, true, "rare_qualified", 2},
	}
	for _, c := range cases {
		if got := cafeQuantityKey(c.grade, c.rare); got != c.key {
			t.Fatalf("grade=%d rare=%v key=%s, want %s", c.grade, c.rare, got, c.key)
		}
		if q[c.key] != c.want {
			t.Fatalf("%s 数量 %d, want %d", c.key, q[c.key], c.want)
		}
	}

	// 缺省周期：长期 24h、奖杯 12h、普通 1~4h。
	p := cafePeriodDefaults()
	if p["longterm"].MinSeconds != 86400 || p["longterm"].MaxSeconds != 86400 {
		t.Fatalf("长期位周期异常: %+v", p["longterm"])
	}
	if p["trophy"].MinSeconds != 43200 || p["trophy"].MaxSeconds != 43200 {
		t.Fatalf("奖杯位周期异常: %+v", p["trophy"])
	}
	if p["normal"].MinSeconds != 3600 || p["normal"].MaxSeconds != 14400 {
		t.Fatalf("普通位周期异常: %+v", p["normal"])
	}

	// 后台配置覆盖缺省。
	cfg := store.CafeBountyConfig{
		Quantities: []store.CafeOrderQuantity{{QKey: "common_normal", Quantity: 7}},
		Periods:    []store.CafeSlotPeriod{{Kind: "normal", MinSeconds: 600, MaxSeconds: 600}},
	}
	qm, pm := cafeQuantityMap(cfg), cafePeriodMap(cfg)
	if qm["common_normal"] != 7 || qm["rare_qualified"] != 2 {
		t.Fatalf("数量配置覆盖失败: %+v", qm)
	}
	if pm["normal"].MinSeconds != 600 || pm["normal"].MaxSeconds != 600 {
		t.Fatalf("周期配置覆盖失败: %+v", pm["normal"])
	}
}

func TestBountyInBand(t *testing.T) {
	if got := bountyInBand(800, 1800, 0); got != 800 {
		t.Fatalf("t=0 应取下限: %d", got)
	}
	if got := bountyInBand(800, 1800, 1); got != 1800 {
		t.Fatalf("t=1 应取上限: %d", got)
	}
	if got := bountyInBand(800, 1800, 2); got != 1800 {
		t.Fatalf("t>1 应钳制: %d", got)
	}
}

// TestCafePoolOldInvalidRowsNoPanic：老玩家库残留旧版本候选池（period_seconds=0）
// 曾导致订单读取整数除零 panic，沃尔霍夫河/苏拉河咖啡厅任务显示为空。
// 新逻辑对非法周期兜底为默认周期，保证所有槽位仍能出单。
func TestCafePoolOldInvalidRowsNoPanic(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	defer db.Close()
	ctx := context.Background()
	for _, level := range []string{"level_005_volhov", "level_008_sura_river"} {
		t.Run(level, func(t *testing.T) {
			// 模拟老库：该水域候选池整批写成旧版 period=0。
			db.SQL.ExecContext(ctx, `DELETE FROM cafe_bounty_pool WHERE level=?`, level)
			rows, err := db.CafeBountyPoolByLevel(ctx, "level_001_lake")
			if err != nil || len(rows) == 0 {
				// 其它水域也无池时，先重建一次拿合法模板。
				srv.RegenerateCafeBountyPool(ctx)
				rows, _ = db.CafeBountyPoolByLevel(ctx, "level_001_lake")
			}
			for _, r := range rows {
				r.Level, r.PeriodSeconds, r.PhaseSeconds = level, 0, 0
				db.SQL.ExecContext(ctx, `INSERT INTO cafe_bounty_pool
                    (level,slot,variant,species,name_zh,weight_g,grade,rarity,is_rare,is_giant,gold_cents,subsidy_cents,period_seconds,phase_seconds,quantity)
                    VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
					r.Level, r.Slot, r.Variant, r.Species, r.NameZH, r.WeightG, r.Grade, r.Rarity,
					cafeBoolInt(r.IsRare), cafeBoolInt(r.IsGiant), r.GoldCents, r.SubsidyCents, 0, 0, r.Quantity)
			}
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("老库 period=0 触发 panic: %v", rec)
				}
			}()
			tpls, err := srv.cafeTemplates(ctx, level, 1730000000)
			if err != nil || len(tpls) == 0 {
				t.Fatalf("老库非法行也应出单: templates=%d err=%v", len(tpls), err)
			}
		})
	}
}

func cafeBoolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
