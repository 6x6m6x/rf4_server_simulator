package gameserver

import (
	"context"
	"testing"
)

// TestCopperLakeUnrestricted 用户补丁核心断言：铜湖服务端限制规则为空，
// 鲤竿/竞技竿/大线杯轮在服务端三入口不再被拒。
func TestCopperLakeUnrestricted(t *testing.T) {
	if !copperRestrictionDisabled {
		t.Skip("补丁开关已关闭，跳过")
	}
	if rules := restrictionsForLevel(copperLakeLevelKey); len(rules) != 0 {
		t.Fatalf("铜湖应无限制规则，实得 %d 条", len(rules))
	}
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPlayerLocation(ctx, uid, copperLakeLevelKey, "", 0); err != nil {
		t.Fatal(err)
	}
	for _, itemID := range []string{"carp_10046_360H", "match_10042_EM18", "spin_7016"} {
		row, err := srv.CreateItem(ctx, uid, itemID, "backpack", 1)
		if err != nil {
			t.Fatalf("%s: %v", itemID, err)
		}
		if reason := srv.placeRodViolation(ctx, uid, row.GUID); reason != "" {
			t.Fatalf("铜湖 %s 不应被服务端拒绝，实得 %q", itemID, reason)
		}
	}
	// 其它地图规则不受影响。
	if rules := restrictionsForLevel("level_000_home"); len(rules) != 0 {
		t.Fatalf("未登记地图不应有规则: %v", rules)
	}
}
