package gameserver

import (
	"context"
	"testing"
	"time"

	"rf4server/internal/store"
)

// 全水域：用户点名的饵即便不在严格清单里，也必须按用户设置给满权重；
// 用户没点名的饵（哪怕在内置清单里）一律不吃。
func TestUserBaitBeatsStrictBuiltinAllLocs(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	ctx := context.Background()
	if err := db.SaveFishBaitOverride(ctx, store.FishBaitOverride{
		FishZh: "常见拟鲤", Allow: []string{"补充饵"}, Enabled: true,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	s.invalidateBaitOverrides()
	setBaitRuleTunable(t, s, "bait_exact_only", "1")
	now := time.Unix(worldDayEpochUnix+1500, 0)
	call := func(ctxLoc string, rt *fishRollRuntime, ids ...string) ([]string, []float64) {
		t.Helper()
		return s.filterFishBaitSet("rules-test", ids, []string{"c.roach"}, []float64{1}, now,
			fishRollContext{loc: ctxLoc, runtime: rt})
	}

	keys, w := call("level_012_north_sea", nil, "rule_other")
	if len(keys) != 1 || w[0] < maxBaitPreference {
		t.Fatalf("挪威海用户饵应压过严格清单并给满权重: keys=%v w=%v", keys, w)
	}
	// 其它水域同样启用用户饵权（本轮扩展到全水域）。
	if keys2, w2 := call("level_900_rules", nil, "rule_other"); len(keys2) != 1 || w2[0] < maxBaitPreference {
		t.Fatalf("其它水域也应启用用户饵权: keys=%v w=%v", keys2, w2)
	}
	// 用户没指定的饵一律不吃（蚯蚓虽在内置严格清单里，也必须被挡）。
	if keys3, _ := call("level_012_north_sea", nil, "rule_worm"); len(keys3) != 0 {
		t.Fatalf("用户未指定的饵不应吃: %v", keys3)
	}
	// 前置规则保留：用户设了海图前置条件但定位不到时，补丁不接管，回原链路。
	if err := db.SaveFishBaitOverride(ctx, store.FishBaitOverride{
		FishZh: "常见拟鲤", Allow: []string{"补充饵"}, Condition: "海图 120 号区域", Enabled: true,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	s.invalidateBaitOverrides()
	rt := &fishRollRuntime{chartKnown: false}
	if keys4, _ := call("level_012_north_sea", rt, "rule_other"); len(keys4) != 0 {
		t.Fatalf("前置条件不满足时不应按用户饵出鱼: %v", keys4)
	}
}

// 挪威海：用户点名的拟饵已上钩时越过 G1；用户没点名的饵鱼直接剔除；
// 其它水域仍按内置 G1 判定。
func TestNorthSeaUserBaitBeatsBiteGate(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	ctx := context.Background()
	if err := db.SaveFishBaitOverride(ctx, store.FishBaitOverride{
		FishZh: "常见拟鲤", Allow: []string{"Hunter 1-001"}, Enabled: true,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	s.invalidateBaitOverrides()
	setBaitRuleTunable(t, s, "bite_gate_enabled", "1")
	setBaitRuleTunable(t, s, "bite_gate_lure_diet_min", "0.6") // 拟鲤 lure≈0.48 < 0.6，G1 必挡
	cfg := s.biteGateConfig()
	if !cfg.enabled {
		t.Fatal("咬口硬门未开启")
	}
	call := func(loc string, ids ...string) []string {
		t.Helper()
		c := fishRollContext{loc: loc, baits: ids}
		keys, _ := s.applyBiteGates(ctx, []string{"c.roach"}, []float64{1}, c, cfg, "rules-test")
		return keys
	}

	if keys := call("level_012_north_sea", "rule_lure"); len(keys) != 1 {
		t.Fatalf("挪威海用户点名拟饵应越过 G1: %v", keys)
	}
	if keys := call("level_012_north_sea", "rule_worm"); len(keys) != 0 {
		t.Fatalf("用户未指定的饵应直接剔除: %v", keys)
	}
	if keys := call("level_900_rules", "rule_lure"); len(keys) != 0 {
		t.Fatalf("非挪威海仍应被 G1 剔除: %v", keys)
	}
}
