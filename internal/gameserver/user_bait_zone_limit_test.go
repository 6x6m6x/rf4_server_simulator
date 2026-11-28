package gameserver

import (
	"context"
	"testing"
	"time"

	"rf4server/internal/store"
)

// TestFreshwaterZoneBaitPriority：淡水区域限制优先于用户饵权。
// 四象限（淡水，用户给某鱼设了「喜爱饵 + 海图区域前置条件」）：
//   用户饵 + 区域内         → 咬钩
//   用户饵 + 区域外         → 不咬钩 ★（旧 bug：回退内置链路仍能咬）
//   非用户饵 + 区域内/区域外 → 不咬钩
// 挪威海维持原语义：condition 不满足回退内置链路，不受本硬门约束。
func TestFreshwaterZoneBaitPriority(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	ctx := context.Background()
	if err := db.SaveFishBaitOverride(ctx, store.FishBaitOverride{
		FishZh: "常见拟鲤", Allow: []string{"补充饵"}, Condition: "海图 120 号区域", Enabled: true,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	s.invalidateBaitOverrides()
	now := time.Unix(worldDayEpochUnix+1500, 0)
	// 不启用 strictExact：让内置链路完整存在，验证「区域外即使回退也不咬」。
	setBaitRuleTunable(t, s, "bait_exact_only", "0")

	call := func(rt *fishRollRuntime, baits ...string) []string {
		t.Helper()
		keys, _ := s.filterFishBaitSet("rules-test", baits, []string{"c.roach"}, []float64{1}, now,
			fishRollContext{loc: "level_900_rules", runtime: rt})
		return keys
	}
	inZone := &fishRollRuntime{chartKnown: true, guideKnown: true, guideSpot: 120}
	outZone := &fishRollRuntime{chartKnown: true, guideKnown: true, guideSpot: 75}

	// 用户饵 + 区域内 → 咬钩，满权重。
	keys, w := s.filterFishBaitSet("rules-test", []string{"rule_other"}, []string{"c.roach"}, []float64{1}, now,
		fishRollContext{loc: "level_900_rules", runtime: inZone})
	if len(keys) != 1 || w[0] < maxBaitPreference {
		t.Fatalf("用户饵+区域内应咬钩且满权重: keys=%v w=%v", keys, w)
	}
	// 用户饵 + 区域外 → 不咬钩（★核心回归）。
	if got := call(outZone, "rule_other"); len(got) != 0 {
		t.Fatalf("用户饵+区域外不应咬钩: %v", got)
	}
	// 非用户饵 + 区域内/区域外 → 都不咬钩。
	if got := call(inZone, "rule_worm"); len(got) != 0 {
		t.Fatalf("非用户饵+区域内不应咬钩: %v", got)
	}
	if got := call(outZone, "rule_worm"); len(got) != 0 {
		t.Fatalf("非用户饵+区域外不应咬钩: %v", got)
	}

	// 挪威海：condition 不满足仍回退原链路（原语义不变，不受本硬门约束）。
	seaOut := &fishRollRuntime{chartKnown: true, guideKnown: true, guideSpot: 75}
	_, has, hit, blocked := s.userBaitRuleForFish(ctx, fishRollContext{loc: "level_012_north_sea", runtime: seaOut},
		"常见拟鲤", []ruleBait{{id: "rule_other", name: "补充饵"}}, now)
	if has || blocked {
		t.Fatalf("挪威海区域外应回退原链路(has=false,blocked=false): has=%t blocked=%t hit=%t", has, blocked, hit)
	}
}
