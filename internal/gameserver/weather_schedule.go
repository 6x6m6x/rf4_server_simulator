package gameserver

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"sort"
	"sync"

	"rf4server/internal/store"
)

const (
	defaultWeatherCycleDays = 64
	minWeatherCycleDays     = 8
	weatherKindCount        = 6
)

// weatherKindWeights 是随机天气 kind 的抽取权重，顺序为晴、少云、多云、
// 雨、暴雨、雷暴。总权重 9：晴天约 1/3，少云约 2/9，其余四类各约 1/9。
var weatherKindWeights = [weatherKindCount]int{3, 2, 1, 1, 1, 1}

// WeatherScheduleOptions 控制进程启动时生成的逐日天气 kind 序列。
// 序列在进程内固定；关闭随机时沿用地图模板的固定顺序。
type WeatherScheduleOptions struct {
	Randomize bool // true=按天气权重重抽 kind；false=沿用模板顺序
	CycleDays int  // 每图序列长度；<=0 用默认 64，小于 8 时按 8 处理
}

// weatherDaySlot 是序列中的一天：kind 决定晴雨，template 指向该图模板中
// 距离该 kind 最近的模板日，用来继承风速和水位；随机模式下 high 是该日
// 独立生成并做过日间限幅的最高温。
type weatherDaySlot struct {
	kind     int64
	template int
	high     float64
}

// 地图原有预报提供气候差异；绝对游戏日决定天气，不随玩家进入时间重抽。
type mapWeatherProfile struct {
	days   []irNode
	hours  [24]irNode
	rangeC float64
	seed   uint64 // 图 key 的稳定哈希，用于降雨过程中心
	offset int64  // 关闭随机时保留模板的日偏移
	random bool   // true=kind 与最高温由启动随机序列提供
	slots  []weatherDaySlot

	// control 后台按图覆盖（天气权重/温度区间/风速档位）。指针便于运行时热更新：
	// 保存配置后整体替换这个指针，判鱼与地图下发立刻读到新值，无需重启。
	controlMu sync.RWMutex
	control   *weatherControlRuntime
}

// weatherControlRuntime 是一张图展开后的运行时天气控制参数。
type weatherControlRuntime struct {
	enabled     bool
	tempBase    float64 // 0=沿用模板区间；>0=覆盖温度基准线
	tempSwing   float64 // 0=沿用模板区间；>0=覆盖上下浮动
	windMode    string  // normal=<5m/s, windy=>=5m/s, calm=0
	kindWeights []float64
}

// defaultWeatherControl 返回与历史行为一致的控制值。
func defaultWeatherControl() *weatherControlRuntime {
	// windMode 留空 = 不覆盖风速（沿用地图模板气候）；只有后台显式保存该图才设档位。
	return &weatherControlRuntime{
		kindWeights: []float64{3, 2, 1, 1, 1, 1},
	}
}

var (
	weatherProfilesMu     sync.RWMutex
	weatherProfiles       map[string]*mapWeatherProfile
	weatherProfilesLoaded bool
	weatherScheduleOpts   WeatherScheduleOptions

	// buildWeatherControls 是构建天气序列时按图读取的后台覆盖；由 server 启动
	// 装配时注入，进程启动后通过 RefreshWeatherControls 热更新（只更新 profile.control）。
	buildWeatherControlsMu sync.RWMutex
	buildWeatherControls   map[string]*weatherControlRuntime
)

// SetWeatherControlsForBuild 注入启动期天气覆盖，供 InitWeatherSchedule 读取。
func SetWeatherControlsForBuild(controls map[string]*weatherControlRuntime) {
	buildWeatherControlsMu.Lock()
	buildWeatherControls = controls
	buildWeatherControlsMu.Unlock()
}

func currentWeatherControlForBuild(mapKey string) *weatherControlRuntime {
	buildWeatherControlsMu.RLock()
	c := buildWeatherControls[mapKey]
	buildWeatherControlsMu.RUnlock()
	return c
}

func mapWeatherProfiles() map[string]*mapWeatherProfile {
	weatherProfilesMu.RLock()
	loaded, profiles := weatherProfilesLoaded, weatherProfiles
	weatherProfilesMu.RUnlock()
	if loaded {
		return profiles
	}
	weatherProfilesMu.Lock()
	defer weatherProfilesMu.Unlock()
	if !weatherProfilesLoaded {
		weatherProfiles = buildWeatherProfiles(weatherScheduleOpts)
		weatherProfilesLoaded = true
	}
	return weatherProfiles
}

// InitWeatherSchedule 在进程启动时生成一次天气序列；之后只读。
// 返回本次启动实际使用的 seed，便于日志排查。
func InitWeatherSchedule(opts WeatherScheduleOptions) (uint64, error) {
	return initWeatherSchedule(opts, 0)
}

// initWeatherSchedule 允许测试注入固定 seed；seed=0 时按启动随机。
func initWeatherSchedule(opts WeatherScheduleOptions, seed uint64) (uint64, error) {
	opts.CycleDays = normalizeWeatherCycleDays(opts.CycleDays)
	if opts.Randomize && seed == 0 {
		var err error
		seed, err = randomWeatherSeed()
		if err != nil {
			return 0, fmt.Errorf("生成天气 seed: %w", err)
		}
	}
	profiles := buildWeatherProfilesWithSeed(opts, seed)
	if len(profiles) == 0 {
		return 0, fmt.Errorf("地图天气模板不可用")
	}
	weatherProfilesMu.Lock()
	weatherProfiles = profiles
	weatherProfilesLoaded = true
	weatherScheduleOpts = opts
	weatherProfilesMu.Unlock()
	return seed, nil
}

// resetWeatherSchedule 清除启动时生成的序列，后续读取回到模板默认顺序。
func resetWeatherSchedule() {
	weatherProfilesMu.Lock()
	weatherProfiles = nil
	weatherProfilesLoaded = false
	weatherScheduleOpts = WeatherScheduleOptions{}
	weatherProfilesMu.Unlock()
}

// clearRandomWeatherSchedule 在配置切回模板天气时清掉同进程中先前生成
// 的随机序列，避免 false 配置继续复用旧 schedule。
func clearRandomWeatherSchedule() {
	weatherProfilesMu.Lock()
	if weatherScheduleOpts.Randomize {
		weatherProfiles = nil
		weatherProfilesLoaded = false
		weatherScheduleOpts = WeatherScheduleOptions{}
	}
	weatherProfilesMu.Unlock()
}

func normalizeWeatherCycleDays(n int) int {
	if n <= 0 {
		return defaultWeatherCycleDays
	}
	if n < minWeatherCycleDays {
		return minWeatherCycleDays
	}
	return n
}

func randomWeatherSeed() (uint64, error) {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}

func buildWeatherProfiles(opts WeatherScheduleOptions) map[string]*mapWeatherProfile {
	return buildWeatherProfilesWithSeed(opts, 0)
}

func buildWeatherProfilesWithSeed(opts WeatherScheduleOptions, seed uint64) map[string]*mapWeatherProfile {
	profiles := make(map[string]*mapWeatherProfile)
	maps, err := loadMapDefs()
	if err != nil {
		return profiles
	}
	cycle := normalizeWeatherCycleDays(opts.CycleDays)
	for key, raw := range maps {
		var root irNode
		if json.Unmarshal(raw, &root) != nil || materializeIR(&root) != nil {
			continue
		}
		p := &mapWeatherProfile{}
		hash := fnv.New64a()
		_, _ = hash.Write([]byte(key))
		p.seed = hash.Sum64()
		low, high := math.Inf(1), math.Inf(-1)
		irWalkObj(&root, func(n *irNode) {
			switch n.Class {
			case forecastClass:
				p.days = append(p.days, *n)
			case hourPlanClass:
				h, ok := irFieldInt(n, 1)
				temp, valid := fishingWeatherFloat(n, 2)
				if ok && valid && h >= 0 && h < 24 {
					p.hours[h] = *n
					low, high = math.Min(low, temp), math.Max(high, temp)
				}
			}
		})
		sort.Slice(p.days, func(i, j int) bool {
			a, _ := irFieldInt(&p.days[i], 0)
			b, _ := irFieldInt(&p.days[j], 0)
			return a < b
		})
		complete := len(p.days) > 0
		for _, h := range p.hours {
			complete = complete && len(h.Fields) == 10
		}
		if !complete {
			continue
		}
		p.rangeC = high - low
		if opts.Randomize {
			p.random = true
			// 生成序列时优先用后台按图权重；缺省仍是官服 3:2:1:1:1:1。
			w := weatherControlFromStore(store.WeatherControl{}, false)
			if s := currentWeatherControlForBuild(key); s != nil {
				w = s
			}
			p.control = w
			p.slots = randomWeatherSlotsWith(p.days, cycle, seed^p.seed, w.kindWeights)
			assignWeatherTemperatures(p.days, p.slots, seed^p.seed, w)
		} else {
			p.offset = int64(p.seed % uint64(len(p.days)))
			p.slots = templateWeatherSlots(p.days)
		}
		profiles[key] = p
	}
	return profiles
}

func templateWeatherSlots(days []irNode) []weatherDaySlot {
	slots := make([]weatherDaySlot, len(days))
	for i := range days {
		kind, _ := irFieldInt(&days[i], 1)
		slots[i] = weatherDaySlot{kind: kind, template: i}
	}
	return slots
}

// randomWeatherSlots 为每张图独立按权重抽取天气 kind。
// 温度、风和水位取模板中距离该 kind 最近的模板日，避免晴天继承暴雨日的参数。
func randomWeatherSlots(days []irNode, cycle int, seed uint64) []weatherDaySlot {
	return randomWeatherSlotsWith(days, cycle, seed, nil)
}

// weatherKindWeightsF 是默认权重的浮点副本。
var weatherKindWeightsF = func() []float64 {
	out := make([]float64, len(weatherKindWeights))
	for i, w := range weatherKindWeights {
		out[i] = float64(w)
	}
	return out
}()

const float64SliceLen = 6

// randomWeatherSlotsWith 按指定权重抽取天气 kind。
func randomWeatherSlotsWith(days []irNode, cycle int, seed uint64, weights []float64) []weatherDaySlot {
	// weights=nil 或恰好等于默认 → 走原 Intn 逻辑，序列与历史完全一致。
	custom := len(weights) == weatherKindCount && !isDefaultKindWeights(weights)
	rng := rand.New(rand.NewSource(int64(seed)))
	slots := make([]weatherDaySlot, cycle)
	for i := range slots {
		kind := int64(0)
		if custom {
			kind = int64(randomWeatherKindWith(rng, weights))
		} else {
			kind = int64(randomWeatherKind(rng))
		}
		slots[i] = weatherDaySlot{kind: kind, template: nearestWeatherTemplate(days, kind)}
	}
	return slots
}

func isDefaultKindWeights(weights []float64) bool {
	for i := range weights {
		if math.Abs(weights[i]-weatherKindWeightsF[i]) > 1e-9 {
			return false
		}
	}
	return true
}

// randomWeatherKind 官服默认权重路径，保留原 Intn 抽取以保证历史天气序列完全一致。
func randomWeatherKind(rng *rand.Rand) int {
	total := 0
	for _, weight := range weatherKindWeights {
		total += weight
	}
	r := rng.Intn(total)
	for kind, weight := range weatherKindWeights {
		if r < weight {
			return kind
		}
		r -= weight
	}
	return weatherKindCount - 1
}

// randomWeatherKindWith 按给定权重抽天气 kind（权重已归一为任意非负浮点）。
func randomWeatherKindWith(rng *rand.Rand, weights []float64) int {
	total := 0.0
	for _, weight := range weights {
		total += math.Max(0, weight)
	}
	if total <= 0 {
		return weatherKindCount - 1
	}
	r := rng.Float64() * total
	for kind, weight := range weights {
		r -= math.Max(0, weight)
		if r < 0 {
			return kind
		}
	}
	return weatherKindCount - 1
}

func nearestWeatherTemplate(days []irNode, kind int64) int {
	best, bestDist := 0, int64(math.MaxInt64)
	for i := range days {
		k, ok := irFieldInt(&days[i], 1)
		if !ok {
			continue
		}
		dist := k - kind
		if dist < 0 {
			dist = -dist
		}
		if dist < bestDist {
			best, bestDist = i, dist
		}
	}
	return best
}

// maxDailyTempSwing 限制相邻两天最高温的变化幅度（°C）。每日温度在
// 模板区间附近随机游走，靠该上限避免逐日大幅震荡；日内昼夜曲线仍由
// hour() 按模板温差生成。
const maxDailyTempSwing = 3.0

// assignRandomTemperatures 为随机序列生成逐日最高温：在模板最高温区间
// 附近做有界随机游走，并向区间均值轻微回归。每天按当前温度限制步长，
// 同时预留返回起始日所需的步数，保证包含首尾的相邻日变化不超过
// maxDailyTempSwing，也不会长期漂出模板气候。
func assignRandomTemperatures(days []irNode, slots []weatherDaySlot, seed uint64) {
	low, high, ok := weatherTemplateHighRange(days)
	if !ok || len(slots) == 0 {
		return
	}
	n := len(slots)
	mean := (low + high) / 2
	margin := maxDailyTempSwing
	minHigh, maxHigh := low-margin, high+margin
	rng := rand.New(rand.NewSource(int64(seed)))
	cur := clampFloat(mean+(rng.Float64()*2-1)*margin, minHigh, maxHigh)
	first := cur
	slots[0].high = cur
	for i := 1; i < n; i++ {
		step := (rng.Float64()*2 - 1) * maxDailyTempSwing
		next := cur + step + (mean-cur)*0.18
		next = clampFloat(next, cur-maxDailyTempSwing, cur+maxDailyTempSwing)
		// 从第 i 天还要走 n-i 步回到第 0 天；把当前值限制在能按时
		// 返回 first 的窗口内，最后一日的首尾相邻差也自然不超过 3°C。
		closeWindow := maxDailyTempSwing * float64(n-i)
		next = clampFloat(next, math.Max(minHigh, first-closeWindow), math.Min(maxHigh, first+closeWindow))
		// 闭环窗口每步收紧 3°C；若当前值正好落在旧窗口边缘，再叠加本次
		// 随机步长会产生 6°C 跳变。窗口收紧后再约束一次相邻日步长。
		next = clampFloat(next, cur-maxDailyTempSwing, cur+maxDailyTempSwing)

		slots[i].high = next
		cur = next
	}
}

func weatherTemplateHighRange(days []irNode) (low, high float64, ok bool) {
	low, high = math.Inf(1), math.Inf(-1)
	for i := range days {
		temp, valid := fishingWeatherFloat(&days[i], 2)
		if !valid {
			continue
		}
		low, high = math.Min(low, temp), math.Max(high, temp)
	}
	return low, high, !math.IsInf(low, 1) && !math.IsInf(high, -1)
}

func copyWeatherNode(n irNode) irNode {
	n.Fields = append([]irField(nil), n.Fields...)
	return n
}

func setWeatherInt(n *irNode, idx int, value int64) {
	n.Fields[idx].V, _ = json.Marshal(value)
}

func setWeatherFloat(n *irNode, idx int, value float64) {
	setWeatherInt(n, idx, int64(math.Float32bits(float32(value))))
}

func (p *mapWeatherProfile) day(day int64) irNode {
	if len(p.slots) == 0 {
		return copyWeatherNode(p.days[0])
	}
	idx := (day + p.offset) % int64(len(p.slots))
	if idx < 0 {
		idx += int64(len(p.slots))
	}
	slot := p.slots[idx]
	n := copyWeatherNode(p.days[slot.template])
	setWeatherInt(&n, 0, day)
	setWeatherInt(&n, 1, slot.kind)
	if p.random {
		setWeatherFloat(&n, 2, slot.high)
	}
	return n
}

func (p *mapWeatherProfile) hour(day, hour int64) irNode {
	daily, next := p.day(day), p.day(day+1)
	kind, _ := irFieldInt(&daily, 1)
	high, _ := fishingWeatherFloat(&daily, 2)
	nextHigh, _ := fishingWeatherFloat(&next, 2)
	wind, _ := fishingWeatherFloat(&daily, 3)
	nextWind, _ := fishingWeatherFloat(&next, 3)
	water, _ := irFieldInt(&daily, 4)
	n := copyWeatherNode(p.hours[hour])
	setWeatherInt(&n, 0, day)
	setWeatherInt(&n, 1, hour)
	// 日出前低温、午后高温；日际温差在一天内缓慢衔接。
	fraction := float64(hour) / 24
	cycle := (1 - math.Cos((float64(hour)-4)*math.Pi/12)) / 2
	temp := high + (nextHigh-high)*fraction - p.rangeC*(1-cycle)
	cloud, rain, thunder := .02, 0.0, 0.0
	switch int(kind) {
	case fishingWeatherFewClouds:
		cloud = .2
	case fishingWeatherCloudy:
		cloud = .6
	case fishingWeatherRain, fishingWeatherHeavyRain, fishingWeatherThunderstorm:
		cloud = .7
		// 每日一次连续天气过程，强度逐小时升降，预报的类型必定达到。
		center := 6 + int((uint64(day)^p.seed)%13)
		distance := math.Abs(float64(hour) - float64(center))
		if distance < 4 {
			envelope := (1 + math.Cos(distance*math.Pi/4)) / 2
			cloud += .25 * envelope
			rain = .4 * envelope
			if int(kind) >= fishingWeatherHeavyRain {
				rain = .9 * envelope
			}
			if int(kind) == fishingWeatherThunderstorm {
				thunder = .8 * envelope
			}
			temp -= 1.5 * envelope
		}
	}
	if p.random {
		// 随机模式把 16 时定为当日峰值，其他小时不高于该值，确保客户端
		// 发布的每日最高温与平滑后的 slot.high 完全一致。
		temp = math.Min(temp, high)
		if hour == 16 {
			temp = high
		}
	}
	// 温度统一取整（°C），不留小数。
	setWeatherFloat(&n, 2, math.Round(temp))
	setWeatherFloat(&n, 3, cloud)
	setWeatherFloat(&n, 4, rain)
	setWeatherFloat(&n, 5, thunder)
	// 预报与小时计划都使用 km/h；吃口入口再换算为 m/s。
	baseWind := math.Max(0, wind+(nextWind-wind)*fraction)
	baseWind = weatherWindWithControl(p, day, hour, baseWind)
	setWeatherFloat(&n, 6, baseWind)
	setWeatherInt(&n, 9, water)
	return n
}

// weatherWindWithControl 按后台风速档位覆盖小时风速（入参 km/h，返回 km/h）。
// 5 m/s（=18 km/h）是大风/正常分界线：normal 落在 [0,5)，windy 落在 [5,12)，
// calm 固定 0（客户端显示无风）。同一天同一小时用固定 seed，保证预报与判鱼一致、
// 且客户端刷新后不会跳变。
func weatherWindWithControl(p *mapWeatherProfile, day, hour int64, templateKMH float64) float64 {
	c := weatherControlOf(p)
	if c == nil || c.windMode == "" {
		return templateKMH
	}
	const divideKMH = 18.0 // 5 m/s
	rng := rand.New(rand.NewSource(int64(p.seed ^ uint64(day*24+hour) ^ 0x5eed)))
	switch c.windMode {
	case "calm":
		return 0
	case "windy":
		return divideKMH + rng.Float64()*7*3.6 // 5~12 m/s
	default: // normal
		return rng.Float64() * divideKMH // 0~5 m/s 不含 5
	}
}

// reanchorWeather 沿用地图协议容器，填入昨天至未来六天的统一天气计划。
func reanchorWeather(root *irNode, nowUnix int64) {
	if root == nil || len(root.Fields) == 0 {
		return
	}
	var key string
	if json.Unmarshal(root.Fields[0].V, &key) != nil {
		return
	}
	p := mapWeatherProfiles()[key]
	if p == nil {
		return
	}
	day, _ := worldDayAndHour(nowUnix)
	var forecasts []*irNode
	var hours *irNode
	irWalkObj(root, func(n *irNode) {
		if n.Class == forecastClass {
			forecasts = append(forecasts, n)
		}
		if len(n.Items) > 0 && n.Items[0].Class == hourPlanClass {
			hours = n
		}
	})
	if hours == nil || len(forecasts) != len(p.days) {
		return
	}
	start, end := day-1, day+int64(len(forecasts))-1
	items := make([]irNode, 0, (end-start)*24+1)
	for d := start; d <= end; d++ {
		forecast := p.day(d)
		maxTemp, maxWind := math.Inf(-1), 0.0
		for h := int64(0); h < 24; h++ {
			n := p.hour(d, h)
			temp, _ := fishingWeatherFloat(&n, 2)
			wind, _ := fishingWeatherFloat(&n, 6)
			maxTemp, maxWind = math.Max(maxTemp, temp), math.Max(maxWind, wind)
			if d < end || h == 0 {
				items = append(items, n)
			}
		}
		if d < end {
			setWeatherFloat(&forecast, 2, maxTemp)
			setWeatherFloat(&forecast, 3, maxWind)
			*forecasts[d-start] = forecast
		}
	}
	hours.Items = items
}

// weatherControlFromStore 把一张图的后台配置展开成运行时参数（含缺省）。
func weatherControlFromStore(w store.WeatherControl, ok bool) *weatherControlRuntime {
	c := defaultWeatherControl()
	if !ok || !w.Enabled {
		return c
	}
	c.enabled = true
	c.tempBase, c.tempSwing = w.TempBase, w.TempSwing
	switch w.WindMode {
	case "calm", "normal", "windy":
		c.windMode = w.WindMode
	}
	if len(w.KindWeights) == 6 {
		weights := make([]float64, 6)
		total := 0.0
		for i, v := range w.KindWeights {
			if v < 0 {
				v = 0
			}
			weights[i] = float64(v)
			total += weights[i]
		}
		if total > 0 {
			c.kindWeights = weights
		}
	}
	return c
}

// RefreshWeatherControls 从玩家库读取天气覆盖并热更新到所有地图 profile。
// 由后台保存配置 / reload_weather 管理指令调用；无 DB 时全部回退默认。
func (s *Server) RefreshWeatherControls(ctx context.Context) error {
	maps := mapWeatherProfiles()
	var rows []store.WeatherControl
	if s != nil && s.DB != nil {
		var err error
		rows, err = s.DB.WeatherControls(ctx)
		if err != nil {
			return err
		}
	}
	byKey := make(map[string]store.WeatherControl, len(rows))
	has := make(map[string]bool, len(rows))
	for _, w := range rows {
		byKey[w.MapKey], has[w.MapKey] = w, true
	}
	weatherProfilesMu.RLock()
	defer weatherProfilesMu.RUnlock()
	for key, p := range maps {
		w, ok := byKey[key], has[key]
		c := weatherControlFromStore(w, ok)
		p.controlMu.Lock()
		p.control = c
		p.controlMu.Unlock()
	}
	return nil
}

// weatherControlOf 读一张图的运行时控制（并发安全，缺省返回默认）。
func weatherControlOf(p *mapWeatherProfile) *weatherControlRuntime {
	if p == nil {
		return defaultWeatherControl()
	}
	p.controlMu.RLock()
	c := p.control
	p.controlMu.RUnlock()
	if c == nil {
		return defaultWeatherControl()
	}
	return c
}

// assignWeatherTemperatures 在默认随机游走基础上应用后台温度覆盖：
//   - tempBase>0：以基准线取代模板区间中值；
//   - tempSwing>0：以 ±swing 取代模板区间半宽；
//   - 所有最高温取整（°C），不再有小数。
// 保留原有的闭环限幅，避免相邻日跳变与漂移。
func assignWeatherTemperatures(days []irNode, slots []weatherDaySlot, seed uint64, c *weatherControlRuntime) {
	if c == nil || (c.tempBase <= 0 && c.tempSwing <= 0) {
		assignRandomTemperatures(days, slots, seed)
		roundSlotHighs(slots)
		return
	}
	low, high, ok := weatherTemplateHighRange(days)
	if !ok || len(slots) == 0 {
		return
	}
	low, high = float64(math.Round(low)), float64(math.Round(high))
	mean := (low + high) / 2
	if c.tempBase > 0 {
		mean = float64(math.Round(c.tempBase))
	}
	half := (high - low) / 2
	if c.tempSwing > 0 {
		half = float64(math.Round(c.tempSwing))
	}
	step := math.Max(1, half/2)
	swing := math.Max(1, math.Min(3, step)) // 单日游走上限，仍保留平滑
	n := len(slots)
	minHigh, maxHigh := mean-half, mean+half
	rng := rand.New(rand.NewSource(int64(seed)))
	cur := clampFloat(mean+(rng.Float64()*2-1)*half*0.6, minHigh, maxHigh)
	first := cur
	slots[0].high = math.Round(cur)
	for i := 1; i < n; i++ {
		next := cur + (rng.Float64()*2-1)*swing + (mean-cur)*0.18
		next = clampFloat(next, cur-swing, cur+swing)
		closeWindow := swing * float64(n-i)
		next = clampFloat(next, math.Max(minHigh, first-closeWindow), math.Min(maxHigh, first+closeWindow))
		next = clampFloat(next, cur-swing, cur+swing)
		slots[i].high = math.Round(next)
		cur = next
	}
}

// roundSlotHighs 把默认随机温度也取整，保证"温度无小数"口径统一。
func roundSlotHighs(slots []weatherDaySlot) {
	for i := range slots {
		slots[i].high = math.Round(slots[i].high)
	}
}

// loadWeatherControls 在 DB 装配后调用：读取后台天气覆盖，注入 build 参数并
// 重建序列（让天气权重/温度区间生效），随后热更新 profile.control（风速档位）。
// 未配置或 DB 不可用时保持现有序列，不改变历史行为。
func (s *Server) loadWeatherControls() {
	if s == nil || s.DB == nil {
		return
	}
	rows, err := s.DB.WeatherControls(context.Background())
	if err != nil {
		if s.log != nil {
			s.log.Warn("天气控制读取失败，沿用默认天气", "err", err)
		}
		return
	}
	controls := make(map[string]*weatherControlRuntime, len(rows))
	for _, w := range rows {
		controls[w.MapKey] = weatherControlFromStore(w, true)
	}
	SetWeatherControlsForBuild(controls)
	if s.cfg.Weather.Randomize {
		if _, err := InitWeatherSchedule(WeatherScheduleOptions{
			Randomize: true,
			CycleDays: s.cfg.Weather.CycleDays,
		}); err != nil && s.log != nil {
			s.log.Warn("天气序列重建失败，沿用原序列", "err", err)
		}
	}
	if err := s.RefreshWeatherControls(context.Background()); err != nil && s.log != nil {
		s.log.Warn("天气控制热更新失败", "err", err)
	}
	if s.log != nil {
		s.log.Info("天气控制已加载", "maps", len(controls))
	}
}
