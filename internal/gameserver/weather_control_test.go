package gameserver

import (
	"encoding/json"
	"math"
	"math/rand"
	"testing"

	"rf4server/internal/store"
)

func TestWeatherControlDefaults(t *testing.T) {
	c := weatherControlFromStore(store.WeatherControl{}, false)
	if c.windMode != "" || len(c.kindWeights) != 6 {
		t.Fatalf("默认控制错误: %+v", c)
	}
	for i, w := range c.kindWeights {
		if w != weatherKindWeightsF[i] {
			t.Fatalf("默认权重 %d = %v, want %v", i, w, weatherKindWeightsF[i])
		}
	}
}

func TestWeatherControlFromStore(t *testing.T) {
	c := weatherControlFromStore(store.WeatherControl{
		Enabled: true, TempBase: 22, TempSwing: 4, WindMode: "windy",
		KindWeights: []int{0, 0, 0, 10, 0, 0},
	}, true)
	if !c.enabled || c.tempBase != 22 || c.tempSwing != 4 || c.windMode != "windy" {
		t.Fatalf("覆盖参数错误: %+v", c)
	}
	if c.kindWeights[3] != 10 || c.kindWeights[0] != 0 {
		t.Fatalf("权重错误: %v", c.kindWeights)
	}
	// 禁用时即使有行也走默认
	off := weatherControlFromStore(store.WeatherControl{Enabled: false, WindMode: "windy"}, true)
	if off.windMode != "" || off.enabled {
		t.Fatal("禁用行不应生效")
	}
}

func TestRandomWeatherKindWithWeights(t *testing.T) {
	// 只给雨天权重 → 必定抽到雨(3)
	r := newTestRNG(1)
	for i := 0; i < 50; i++ {
		got := randomWeatherKindWith(r, []float64{0, 0, 0, 5, 0, 0})
		if got != 3 {
			t.Fatalf("权重=只雨却抽到 %d", got)
		}
	}
	// 全 0 → 兜底雷暴
	if got := randomWeatherKindWith(newTestRNG(1), []float64{0, 0, 0, 0, 0, 0}); got != weatherKindCount-1 {
		t.Fatalf("全0权重兜底错误: %d", got)
	}
}

func TestWeatherWindBands(t *testing.T) {
	p := &mapWeatherProfile{seed: 12345}
	p.control = &weatherControlRuntime{windMode: "calm"}
	for d := int64(0); d < 10; d++ {
		if got := weatherWindWithControl(p, d, 12, 30); got != 0 {
			t.Fatalf("calm 应为 0, got %v", got)
		}
	}
	p.control = &weatherControlRuntime{windMode: "normal"}
	for d := int64(0); d < 50; d++ {
		ms := weatherWindWithControl(p, d, 12, 30) / 3.6
		if ms < 0 || ms >= 5 {
			t.Fatalf("normal 应 0~5m/s, got %v", ms)
		}
	}
	p.control = &weatherControlRuntime{windMode: "windy"}
	for d := int64(0); d < 50; d++ {
		ms := weatherWindWithControl(p, d, 12, 30) / 3.6
		if ms < 5 || ms >= 12 {
			t.Fatalf("windy 应 5~12m/s, got %v", ms)
		}
	}
}

func TestWeatherTemperaturesAreIntegers(t *testing.T) {
	slots := []weatherDaySlot{{high: 18.37}, {high: 21.9}, {high: -0.5}}
	roundSlotHighs(slots)
	for _, s := range slots {
		if math.Abs(s.high-math.Round(s.high)) > 1e-9 {
			t.Fatalf("温度应为整数, got %v", s.high)
		}
	}
	// 用真实地图模板验证温度覆盖落在基准±浮动内且为整数
	maps, err := loadMapDefs()
	if err != nil {
		t.Skip("地图模板不可用")
	}
	raw := maps["level_014_carp_lake"]
	var root irNode
	if json.Unmarshal(raw, &root) != nil || materializeIR(&root) != nil {
		t.Skip("地图模板解析失败")
	}
	var days []irNode
	irWalkObj(&root, func(n *irNode) { if n.Class == forecastClass { days = append(days, *n) } })
	if len(days) == 0 {
		t.Skip("无预报节点")
	}
	realSlots := make([]weatherDaySlot, 8)
	for i := range realSlots { realSlots[i] = weatherDaySlot{kind: 0, template: i % len(days)} }
	c := &weatherControlRuntime{enabled: true, tempBase: 20, tempSwing: 3}
	assignWeatherTemperatures(days, realSlots, 42, c)
	for _, s := range realSlots {
		if math.Abs(s.high-math.Round(s.high)) > 1e-9 || s.high < 17 || s.high > 23 {
			t.Fatalf("温度应整数且在 20±3, got %v", s.high)
		}
	}
}

func newTestRNG(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }
