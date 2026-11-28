package gameserver

import (
	"context"
	"testing"
	"time"

	"rf4server/internal/store"
)

// TestUserBaitFollowsMainlineSemantics 验证「去掉用户饵权最高补丁、回归主分支原生方案」后的语义：
//  1. 目标鱼只吃用户设置的饵（allow 清单替换内置规则），没点名的饵不吃；
//  2. 主分支生态前置照常生效（区域/温度/昼夜经 condition 表达，不满足不开口）；
//  3. 用户没设规则的鱼完全走内置链路，不受影响。
//
// 不再有「满档 1.725 跳过占比分级/长尾闸/食性硬门」的补丁快通道。
func TestUserBaitFollowsMainlineSemantics(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	ctx := context.Background()
	if err := db.SaveFishBaitOverride(ctx, store.FishBaitOverride{
		FishZh: "常见拟鲤", Allow: []string{"补充饵"}, Enabled: true,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	s.invalidateBaitOverrides()
	setBaitRuleTunable(t, s, "bait_exact_only", "0")
	setBaitRuleTunable(t, s, "bait_share_strong", "0") // 关闭占比分级，隔离测试
	setBaitRuleTunable(t, s, "bait_share_medium", "0")
	setBaitRuleTunable(t, s, "bait_share_tail", "0")
	now := time.Unix(worldDayEpochUnix+1500, 0)

	call := func(ids ...string) []string {
		t.Helper()
		keys, _ := s.filterFishBaitSet("rules-test", ids, []string{"c.roach"}, []float64{1}, now,
			fishRollContext{loc: "level_900_rules"})
		return keys
	}
	// 用户点名的饵 → 咬；没点名的饵（含内置清单里的蚯蚓）→ 不吃。
	if got := call("rule_other"); len(got) != 1 {
		t.Fatalf("用户点名饵应咬: %v", got)
	}
	if got := call("rule_worm"); len(got) != 0 {
		t.Fatalf("用户未点名饵不应吃: %v", got)
	}
	// 主分支生态前置：用户规则带海图条件但无证据 → 该鱼不开口（condition 照常判定）。
	if err := db.SaveFishBaitOverride(ctx, store.FishBaitOverride{
		FishZh: "常见拟鲤", Allow: []string{"补充饵"}, Condition: "海图 120 号区域", Enabled: true,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	s.invalidateBaitOverrides()
	rt := &fishRollRuntime{chartKnown: false}
	keys, _ := s.filterFishBaitSet("rules-test", []string{"rule_other"}, []string{"c.roach"}, []float64{1}, now,
		fishRollContext{loc: "level_900_rules", runtime: rt})
	if len(keys) != 0 {
		t.Fatalf("生态前置(海图区域)不满足时不应咬: %v", keys)
	}

	// 用户没设规则的鱼（青蛙）走内置链路：内置规则允许蚯蚓时照常咬，不受用户 override 影响。
	keys2, _ := s.filterFishBaitSet("rules-test", []string{"rule_worm"}, []string{"frog"}, []float64{1}, now,
		fishRollContext{loc: "level_900_rules"})
	if len(keys2) != 1 {
		t.Fatalf("未设用户规则的鱼应走内置链路: %v", keys2)
	}
}
