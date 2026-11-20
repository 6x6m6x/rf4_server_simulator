package gameserver

import "testing"

// TestMarineFishLayerUnrestricted：挪威海鱼种的水层限制被彻底解除
// （生态软权重恒 1、硬门 G2 不再乘地板）；淡水鱼及其它规则不受影响。
func TestMarineFishLayerUnrestricted(t *testing.T) {
	srv := &Server{}
	srv.marineFishMu.Lock()
	srv.marineFishKeys = map[string]bool{"atl_cod": true, "halibut_w": true}
	srv.marineFishMu.Unlock()

	// 构造两种鱼：海鱼(上层偏好)与淡水鱼(上层偏好)，水层区间相同。
	marineP := fishEcologyProfile{FishKey: "atl_cod", Enabled: true, LayerMin: .05, LayerPref: .1, LayerMax: .3}
	freshP := fishEcologyProfile{FishKey: "n.pike", Enabled: true, LayerMin: .05, LayerPref: .1, LayerMax: .3}

	// 底层钓法（target 远离上层鱼的偏好区间）：淡水鱼被衰减，海鱼恒 1。
	bottom := fishRollContext{rigType: 12, depthCM: 500} // 底层
	if got := srv.fishLayerWeightProfile(freshP, bottom); got >= 1 {
		t.Fatalf("淡水鱼在错配水层应被降权, got=%f", got)
	}
	for _, layer := range []fishRollContext{
		{rigType: 12, depthCM: 500},
		{rigType: 1, depthCM: 50},
		{rigType: 5, lureLayer: .5},
	} {
		if got := srv.fishLayerWeightProfile(marineP, layer); got != 1 {
			t.Fatalf("挪威海鱼种水层权重应恒 1, got=%f rig=%d", got, layer.rigType)
		}
	}

	// 非海鱼仍走原逻辑：区间内≈1，区间外被压低。
	inside := fishRollContext{rigType: 1, depthCM: 50}
	if got := srv.fishLayerWeightProfile(freshP, inside); got <= .5 {
		t.Fatalf("淡水鱼在匹配水层应高权重, got=%f", got)
	}
}
