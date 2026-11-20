package gameserver

import "testing"

// TestMarineRigMapLimit：海洋钓组(rigType 50-60，"海洋钓鱼"技能内)只能在
// 挪威海(level_012_north_sea)上鱼；其它地图/其它钓组不受影响（官服行为）。
func TestMarineRigMapLimit(t *testing.T) {
	sea := marineOnlyLevelKey
	fresh := "level_001_lake"
	// 海洋钓组 50-60：海图放行，淡水图禁止。
	for _, rig := range []int64{50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60} {
		if marineBlockedOnLevel(rig, sea) {
			t.Fatalf("海洋钓组 %d 在挪威海应可上鱼", rig)
		}
		if !marineBlockedOnLevel(rig, fresh) {
			t.Fatalf("海洋钓组 %d 在淡水图应被禁止", rig)
		}
	}
	// 非海洋钓组：任意地图都不受影响。
	for _, rig := range []int64{1, 5, 12, 33, 35, 49, 61} {
		if marineBlockedOnLevel(rig, sea) || marineBlockedOnLevel(rig, fresh) {
			t.Fatalf("非海洋钓组 %d 不应被限制", rig)
		}
	}
	// 兜底：currentLevel 返回值经由 travelLevelKey 归一，海图常量须与之同源。
	if sea != "level_012_north_sea" {
		t.Fatalf("海图常量错误: %s", sea)
	}
}
