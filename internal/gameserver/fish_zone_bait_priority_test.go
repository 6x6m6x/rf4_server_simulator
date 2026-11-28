package gameserver

import (
	"context"
	"testing"
	"time"

	"rf4server/internal/store"
)

// TestFishZoneExclusiveBeatsUserBaitPriority：用户喜爱饵只改饵权层，
// 不能把被后台鱼种区域独占区剔除的鱼加回来。
func TestFishZoneExclusiveBeatsUserBaitPriority(t *testing.T) {
	s, db := seedFishZoneTest(t)
	ctx := context.Background()
	saveZoneForTest(t, db, store.FishZone{
		Loc: "rules-test", Name: "青蛙专区", Kind: "grid_rect",
		GX0: 2, GZ0: 2, GX1: 4, GZ1: 4, Mode: "exclusive", Enabled: true,
		Fish: []store.FishZoneFish{{FishKey: "frog", Weight: 1, Enabled: true}},
	})
	if err := db.SaveFishBaitOverride(ctx, store.FishBaitOverride{
		FishZh: "常见拟鲤", Allow: []string{"补充饵"}, Enabled: true,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	s.invalidateBaitOverrides()
	reloadZonesForTest(t, s)
	c := fishRollContext{baits: []string{"rule_other"}, now: time.Now(), x: 100, z: 100, hasPos: true, loc: "level_900_rules"}
	for range 64 {
		if f := s.rollFishWithRules("level_900_rules", "rule_other", 0, fishingSkillBonus{BiteSpeed: 1}, c); f != nil && f.Species == "c.roach" {
			t.Fatalf("用户饵权不应绕过独占区名册，却钓到了区外的 %q", f.Species)
		}
	}
}

// TestDistributionShareIgnoresBaitBoost：鱼群散布的缺席判定线必须用
// 饵权加成之前的区域份额算。用户喜爱饵的满档 1.725 把 share 抬过 5%
// 时，旧逻辑会让稀有鱼的缺席线对折，本该「本时段不来」的鱼变成常驻。
func TestDistributionShareIgnoresBaitBoost(t *testing.T) {
	setFishRaritySnapshot(map[string]int64{"rare_probe": 2})
	defer setFishRaritySnapshot(nil)
	keys := []string{"rare_probe", "common_probe"}
	base := []float64{1, 30}    // rare share = 3.2% < 5% → 缺席线 0.10
	boosted := []float64{1.725, 30} // 用户饵满档后 share = 5.4% ≥ 5% → 缺席线 0.05
	found := false
	for period := int64(0); period < 200; period++ {
		at := time.Unix(worldDayEpochUnix+period*distributionPeriodRealHours*3600, 0)
		c := fishRollContext{now: at, hasPos: true, x: 50, z: 50}
		wBase := append([]float64(nil), base...)
		applyFishDistributionWithBase("probe", keys, wBase, base, c)
		wBoost := append([]float64(nil), boosted...)
		applyFishDistribution("probe", keys, wBoost, c) // 旧行为：用加成后权重算 share
		if wBase[0] == 0 && wBoost[0] > 0 {
			found = true // 找到一条能区分两种口径的时段
			break
		}
	}
	if !found {
		t.Fatal("未找到可区分 share 口径的时段，测试样本不足")
	}
}
