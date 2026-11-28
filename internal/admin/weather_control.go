package admin

import (
	"net/http"
	"strconv"

	"rf4server/internal/store"
)

// weatherMapNames 18 张地图的中文名（展示用；key 与服务端 map_defs 一致）。
var weatherMapNames = map[string]string{
	"level_001_lake":          "湖泊",
	"level_002_kirzah_river":  "基尔扎河",
	"level_003_viiveri_lake":  "维韦里湖",
	"level_004_torfyanoe_ozero": "泥炭湖",
	"level_005_volhov":        "沃尔霍夫河",
	"level_006_ladoga":        "拉多加湖",
	"level_007_grass_lake":    "草湖",
	"level_008_sura_river":    "苏拉河",
	"level_009_ahtuba":        "阿赫图巴河",
	"level_010_belaya":        "白河",
	"level_011_ladoga_02":     "拉多加湖 II",
	"level_012_north_sea":     "挪威海",
	"level_013_sev_don":       "北顿河",
	"level_014_carp_lake":     "琥珀湖（鲤鱼湖）",
	"level_015_tunguska":      "通古斯卡河",
	"level_016_yama":          "亚玛湖",
	"level_018_moskitnoe":     "蚊子湖",
	"level_019_american_pond": "美国池塘",
}

// handleWeatherMapList GET /api/weather/maps —— 全部地图 + 当前控制配置（匿名只读）。
func (s *Server) handleWeatherMapList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.WeatherControls(r.Context())
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	byKey := make(map[string]store.WeatherControl, len(rows))
	for _, row := range rows {
		byKey[row.MapKey] = row
	}
	type item struct {
		MapKey string `json:"map_key"`
		Name   string `json:"name"`
		Config *store.WeatherControl `json:"config,omitempty"`
	}
	list := make([]item, 0, len(weatherMapNames))
	for key, name := range weatherMapNames {
		it := item{MapKey: key, Name: name}
		if c, ok := byKey[key]; ok {
			cc := c
			it.Config = &cc
		}
		list = append(list, it)
	}
	writeJSON(w, map[string]any{"items": list})
}

// handleWeatherControlSave PUT /api/weather/control —— 保存一张图的天气控制。
func (s *Server) handleWeatherControlSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MapKey      string `json:"map_key"`
		TempBase    int    `json:"temp_base"`
		TempSwing   int    `json:"temp_swing"`
		WindMode    string `json:"wind_mode"`
		KindWeights []int  `json:"kind_weights"`
		Enabled     bool   `json:"enabled"`
	}
	if err := readJSON(r, &req); err != nil || req.MapKey == "" {
		httpError(w, 400, "参数错误")
		return
	}
	if _, ok := weatherMapNames[req.MapKey]; !ok {
		httpError(w, 400, "未知地图")
		return
	}
	if req.TempBase < -50 || req.TempBase > 60 || req.TempSwing < 0 || req.TempSwing > 40 {
		httpError(w, 400, "温度基准需在 -50~60、浮动 0~40")
		return
	}
	switch req.WindMode {
	case "calm", "normal", "windy":
	default:
		httpError(w, 400, "风速档位无效")
		return
	}
	if len(req.KindWeights) != 6 {
		httpError(w, 400, "天气权重必须是 6 项")
		return
	}
	for _, v := range req.KindWeights {
		if v < 0 || v > 100 {
			httpError(w, 400, "天气权重需在 0~100")
			return
		}
	}
	wCtl := store.WeatherControl{
		MapKey: req.MapKey, TempBase: float64(req.TempBase), TempSwing: float64(req.TempSwing),
		WindMode: req.WindMode, KindWeights: req.KindWeights, Enabled: req.Enabled,
	}
	if err := s.db.SaveWeatherControl(r.Context(), wCtl, adminOf(r)); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.db.QueuePush(r.Context(), mustJSON(map[string]any{"op": "reload_weather"}))
	_ = s.db.AuditLogWrite(r.Context(), adminOf(r), "weather_control_save", req.MapKey,
		strconv.Itoa(req.TempBase)+"/"+req.WindMode)
	writeJSON(w, map[string]any{"ok": true, "queued": true})
}

// handleWeatherControlReset DELETE /api/weather/control?map=xxx —— 删除覆盖恢复默认。
func (s *Server) handleWeatherControlReset(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("map")
	if key == "" {
		httpError(w, 400, "缺少 map")
		return
	}
	if err := s.db.DeleteWeatherControl(r.Context(), key); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.db.QueuePush(r.Context(), mustJSON(map[string]any{"op": "reload_weather"}))
	_ = s.db.AuditLogWrite(r.Context(), adminOf(r), "weather_control_reset", key, "")
	writeJSON(w, map[string]any{"ok": true, "queued": true})
}
