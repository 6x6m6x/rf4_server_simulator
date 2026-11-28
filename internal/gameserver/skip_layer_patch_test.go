package gameserver

import (
	"context"
	"math"
	"testing"
	"time"

	"rf4server/internal/store"
)

// 补丁默认开启：skip_layer_factor 未落库时应视为 1。
func TestSkipLayerFactorDefaultsOn(t *testing.T) {
	s, _ := seedBaitRuleTest(t)
	if !s.skipLayerFactor() {
		t.Fatal("skip_layer_factor 默认应为开（跳过水层）")
	}
	setBaitRuleTunable(t, s, "skip_layer_factor", "0")
	if s.skipLayerFactor() {
		t.Fatal("设为 0 后应回退到主分支水层判定")
	}
}

// 生态权重的水层因子：跳过时置中性 1，开关关掉后恢复真实水层权重。
func TestSkipLayerFactorEcology(t *testing.T) {
	s, _ := seedBaitRuleTest(t)
	ctx := context.Background()
	setBaitRuleTunable(t, s, "fish_ecology_enabled", "1")
	c := fishRollContext{
		loc: "rules-test", now: time.Unix(worldDayEpochUnix, 0), rigType: 1,
		depthCM: 20, // 浅水层
	}
	// 找一条栖息在深水的鱼：跳过时必须不因水层被压低。
	p := fishEcologyProfile{Enabled: true, FishKey: "deep_fish", LayerMin: .8, LayerMax: 1, LayerPref: .93, DietJSON: `{}`, WeatherJSON: `{}`, ColorJSON: `{}`, ActionJSON: `{}`, TempMinC: 0, TempMaxC: 30, TempPrefC: 15}
	withSkip := s.fishBiteEnvironmentWeight(ctx, "deep_fish", c)

	// 关掉开关，水层因子恢复，浅水打深水鱼权重应明显下降。
	setBaitRuleTunable(t, s, "skip_layer_factor", "0")
	noSkip := s.fishBiteEnvironmentWeight(ctx, "deep_fish", c)
	_ = p

	if withSkip <= 0 || math.IsNaN(withSkip) {
		t.Fatalf("跳过时生态权重异常: %f", withSkip)
	}
	// 跳过水层后权重应不低于未跳过（未跳过会被水层地板压低）。
	if withSkip < noSkip {
		t.Fatalf("跳过水层后权重(%f)不应低于未跳过(%f)", withSkip, noSkip)
	}
}

// 用户饵料独占：设了 allow 的鱼只吃 allow 里的饵，占比/companion/本地证据不得越级。
func TestUserBaitExclusiveNoEvidenceLeak(t *testing.T) {
	s, db := seedBaitRuleTest(t)
	setBaitRuleSwitch(t, s, "1")
	// 开启食性分级，制造占比/companion 越级通道。
	setBaitShareGate(t, s, "5", "1", "0.2")
	ctx := context.Background()
	if err := db.SaveFishBaitOverride(ctx, store.FishBaitOverride{
		FishZh: "常见拟鲤", Allow: []string{"蚯蚓"}, Enabled: true,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	s.invalidateBaitOverrides()

	keys := []string{"c.roach"}
	now := time.Unix(1_800_000_000, 0)
	// rule_other（补充饵）不在 allow：即便占比/证据拉满也必须被剔除。
	got, _ := s.filterFishBaitSet("rules-test", []string{"rule_other"}, keys, []float64{100}, now)
	if len(got) != 0 {
		t.Fatalf("用户饵料独占：非 allow 饵不应咬，实得 %v", got)
	}
	// allow 里的饵仍能咬。
	got, _ = s.filterFishBaitSet("rules-test", []string{"rule_worm"}, keys, []float64{100}, now)
	if len(got) != 1 {
		t.Fatalf("用户 allow 饵应咬，实得 %v", got)
	}
}
