package gameserver

import (
	"context"
	"testing"
	"time"

	"rf4server/internal/store"
)

// TestFreshwaterZoneBaitPriority：淡水区域(fish_zone 落点名册)优先于用户饵权。
//
// 淡水与挪威海的区域是两套东西：
//   淡水 = fish_zone 独占区（grid_rect 落点坐标），被名单收编的鱼只能在区内咬；
//   挪威海 = 用户饵规则的海图区号 condition，不满足回退内置链路。
//
// 四象限（淡水，用户给「常见拟鲤」设了喜爱饵，且 fish_zone 独占区把拟鲤
// 限定在 (2..4, 2..4) 格）：
//   用户饵 + 区域内 → 咬钩
//   用户饵 + 区域外 → 不咬钩 ★（重写核心：用户饵权不突破区域）
//   非用户饵 + 区域内/区域外 → 不咬钩（用户规则生效时只认用户饵）
func TestFreshwaterZoneBaitPriority(t *testing.T) {
	s, db := seedFishZoneTest(t)
	ctx := context.Background()
	// 独占区把拟鲤限定在 (2..4,2..4) 格（40m/格 → x,z ∈ [80,200)）。
	saveZoneForTest(t, db, store.FishZone{
		Loc: "rules-test", Name: "拟鲤专区", Kind: "grid_rect",
		GX0: 2, GZ0: 2, GX1: 4, GZ1: 4, Mode: "exclusive", Enabled: true,
		Fish: []store.FishZoneFish{{FishKey: "c.roach", Weight: 1, Enabled: true}},
	})
	if err := db.SaveFishBaitOverride(ctx, store.FishBaitOverride{
		FishZh: "常见拟鲤", Allow: []string{"补充饵"}, Enabled: true,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	s.invalidateBaitOverrides()
	reloadZonesForTest(t, s)
	setBaitRuleTunable(t, s, "bait_exact_only", "0")
	setBaitRuleSwitch(t, s, "1") // 启用规则开关，走主链路（用户饵权才会介入）
	now := time.Unix(worldDayEpochUnix+1500, 0)

	baits := []ruleBait{{id: "rule_other", name: "补充饵"}}
	inZone := fishRollContext{loc: "level_900_rules", x: 100, z: 100, hasPos: true, now: now}
	outZone := fishRollContext{loc: "level_900_rules", x: 400, z: 400, hasPos: true, now: now}
	noPos := fishRollContext{loc: "level_900_rules", hasPos: false, now: now} // 无落点证据 → 视为区外

	// verdict 直测。
	_, _, v := s.evaluateUserBait(ctx, inZone, "rules-test", "c.roach", "常见拟鲤", baits, now)
	if v != userBaitHit {
		t.Fatalf("用户饵+区域内应 hit, got %v", v)
	}
	_, _, v = s.evaluateUserBait(ctx, outZone, "rules-test", "c.roach", "常见拟鲤", baits, now)
	if v != userBaitBlocked {
		t.Fatalf("用户饵+区域外应 blocked, got %v", v)
	}
	_, _, v = s.evaluateUserBait(ctx, noPos, "rules-test", "c.roach", "常见拟鲤", baits, now)
	if v != userBaitBlocked {
		t.Fatalf("用户饵+无落点证据应 blocked, got %v", v)
	}
	// 非用户饵：区域内也 miss（用户规则生效只认用户饵）。
	_, _, v = s.evaluateUserBait(ctx, inZone, "rules-test", "c.roach", "常见拟鲤",
		[]ruleBait{{id: "rule_worm", name: "红虫"}}, now)
	if v != userBaitMiss {
		t.Fatalf("非用户饵+区域内应 miss, got %v", v)
	}

	// 端到端：区域外 64 次判鱼都钓不到被独占区收编的拟鲤。
	for range 64 {
		c := fishRollContext{baits: []string{"rule_other"}, now: now, x: 400, z: 400, hasPos: true, loc: "level_900_rules"}
		if f := s.rollFishWithRules("level_900_rules", "rule_other", 0, fishingSkillBonus{BiteSpeed: 1}, c); f != nil && f.Species == "c.roach" {
			t.Fatalf("用户饵权不应绕过独占区名册，却钓到了区外的 %q", f.Species)
		}
	}

	// 挪威海：区域走海图区号 condition，不满足回退内置链路（None），不受 fish_zone 影响。
	seaOut := &fishRollRuntime{chartKnown: true, guideKnown: true, guideSpot: 75}
	if err := db.SaveFishBaitOverride(ctx, store.FishBaitOverride{
		FishZh: "常见拟鲤", Allow: []string{"补充饵"}, Condition: "海图 120 号区域", Enabled: true,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	s.invalidateBaitOverrides()
	_, _, v = s.evaluateUserBait(ctx, fishRollContext{loc: "level_012_north_sea", runtime: seaOut},
		"level_012_north_sea", "c.roach", "常见拟鲤", baits, now)
	if v != userBaitNone {
		t.Fatalf("挪威海区域外应回退原链路(None), got %v", v)
	}
}
