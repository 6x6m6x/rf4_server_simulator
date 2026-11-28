package store

import (
	"context"
	"encoding/json"
	"time"
)

// WeatherControl 一张地图的天气控制参数。
// TempBase/TempSwing 为 0 表示沿用模板气候；WindMode 取值 normal/windy/calm。
// KindWeights 是 6 种天气（晴/少云/多云/雨/暴雨/雷暴）的抽取权重，空表示默认 3,2,1,1,1,1。
type WeatherControl struct {
	MapKey      string  `json:"map_key"`
	TempBase    float64 `json:"temp_base"`
	TempSwing   float64 `json:"temp_swing"`
	WindMode    string  `json:"wind_mode"`
	KindWeights []int   `json:"kind_weights"`
	Enabled     bool    `json:"enabled"`
	UpdatedBy   string  `json:"updated_by"`
}

// WeatherControls 读取全部天气控制行（含禁用行，交给上层决定）。
func (d *DB) WeatherControls(ctx context.Context) ([]WeatherControl, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT map_key,temp_base,temp_swing,wind_mode,kind_weights,enabled,updated_by FROM weather_control`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]WeatherControl, 0, 32)
	for rows.Next() {
		var w WeatherControl
		var weights string
		var enabled int
		if err := rows.Scan(&w.MapKey, &w.TempBase, &w.TempSwing, &w.WindMode,
			&weights, &enabled, &w.UpdatedBy); err != nil {
			continue
		}
		w.Enabled = enabled != 0
		if weights != "" {
			_ = json.Unmarshal([]byte(weights), &w.KindWeights)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// WeatherControlFor 读取单图配置；不存在时返回 ok=false。
func (d *DB) WeatherControlFor(ctx context.Context, mapKey string) (WeatherControl, bool, error) {
	var w WeatherControl
	var weights string
	var enabled int
	err := d.SQL.QueryRowContext(ctx,
		`SELECT map_key,temp_base,temp_swing,wind_mode,kind_weights,enabled,updated_by FROM weather_control WHERE map_key=?`,
		mapKey).Scan(&w.MapKey, &w.TempBase, &w.TempSwing, &w.WindMode,
		&weights, &enabled, &w.UpdatedBy)
	if err != nil {
		return w, false, err
	}
	w.Enabled = enabled != 0
	if weights != "" {
		_ = json.Unmarshal([]byte(weights), &w.KindWeights)
	}
	return w, true, nil
}

// SaveWeatherControl 写入/更新一张图的天气控制参数。
func (d *DB) SaveWeatherControl(ctx context.Context, w WeatherControl, by string) error {
	weights := ""
	if len(w.KindWeights) == 6 {
		if b, err := json.Marshal(w.KindWeights); err == nil {
			weights = string(b)
		}
	}
	enabled := 0
	if w.Enabled {
		enabled = 1
	}
	_, err := d.SQL.ExecContext(ctx, `
		INSERT INTO weather_control(map_key,temp_base,temp_swing,wind_mode,kind_weights,enabled,updated_ts,updated_by)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(map_key) DO UPDATE SET
		  temp_base=excluded.temp_base, temp_swing=excluded.temp_swing,
		  wind_mode=excluded.wind_mode, kind_weights=excluded.kind_weights,
		  enabled=excluded.enabled, updated_ts=excluded.updated_ts,
		  updated_by=excluded.updated_by`,
		w.MapKey, w.TempBase, w.TempSwing, w.WindMode, weights, enabled, time.Now().Unix(), by)
	return err
}

// DeleteWeatherControl 删除一张图的覆盖配置，恢复默认。
func (d *DB) DeleteWeatherControl(ctx context.Context, mapKey string) error {
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM weather_control WHERE map_key=?`, mapKey)
	return err
}
