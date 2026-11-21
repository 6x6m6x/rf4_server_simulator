package admin

import (
	"fmt"
	"net/http"
	"strings"

	"rf4server/internal/store"
)

// handleCafeConfig GET 咖啡厅悬赏配置（金币档位/巨物补贴/交单数量/轮换周期/槽位占比/黑名单）。
// 白名单已固化到 gameserver 静态文件，不在配置内返回。
func (s *Server) handleCafeConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.db.CafeBountyConfig(r.Context())
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, cfg)
}

// handleCafeBandSet POST 更新一个档位金币范围（金币分）。
func (s *Server) handleCafeBandSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
		Lo  int64  `json:"lo_cents"`
		Hi  int64  `json:"hi_cents"`
	}
	if err := readJSON(r, &req); err != nil || req.Key == "" {
		httpError(w, 400, "参数错误")
		return
	}
	// 仅允许更新已知档位键。
	valid := map[string]bool{"normal_common": true, "normal_rare": true, "trophy_common": true,
		"trophy_rare": true, "blue_common": true, "blue_rare": true}
	if !valid[req.Key] {
		httpError(w, 400, "未知档位: "+req.Key)
		return
	}
	if err := s.db.CafeBountyBandSet(r.Context(), req.Key, req.Lo, req.Hi); err != nil {
		httpError(w, 400, err.Error())
		return
	}
	_ = s.db.AuditLogWrite(r.Context(), adminOf(r), "cafe_band", req.Key, fmt.Sprintf("lo=%d hi=%d", req.Lo, req.Hi))
	s.pushCafeReload(r)
	writeJSON(w, map[string]any{"ok": true})
}

// handleCafeBlacklistAdd POST 添加一条按水域的悬赏黑名单。
func (s *Server) handleCafeBlacklistAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level   string `json:"level"`
		FishKey string `json:"fish_key"`
		NameZH  string `json:"name_zh"`
	}
	if err := readJSON(r, &req); err != nil {
		httpError(w, 400, "参数错误")
		return
	}
	b := store.CafeBlacklist{Level: strings.TrimSpace(req.Level), FishKey: strings.TrimSpace(req.FishKey),
		NameZH: strings.TrimSpace(req.NameZH)}
	if err := s.db.CafeBlacklistAdd(r.Context(), b); err != nil {
		httpError(w, 400, err.Error())
		return
	}
	_ = s.db.AuditLogWrite(r.Context(), adminOf(r), "cafe_blacklist_add", b.Level+"/"+b.FishKey, b.NameZH)
	s.pushCafeReload(r)
	writeJSON(w, map[string]any{"ok": true})
}

// handleCafeBlacklistRemove POST 删除一条按水域的悬赏黑名单。
func (s *Server) handleCafeBlacklistRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level   string `json:"level"`
		FishKey string `json:"fish_key"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Level) == "" || strings.TrimSpace(req.FishKey) == "" {
		httpError(w, 400, "参数错误")
		return
	}
	level, key := strings.TrimSpace(req.Level), strings.TrimSpace(req.FishKey)
	if err := s.db.CafeBlacklistRemove(r.Context(), level, key); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	_ = s.db.AuditLogWrite(r.Context(), adminOf(r), "cafe_blacklist_remove", level+"/"+key, "")
	s.pushCafeReload(r)
	writeJSON(w, map[string]any{"ok": true})
}

// handleCafeFishSearch GET 按中文名/鱼种 key 模糊搜索可钓鱼种，供黑名单添加窗口自动补全。
func (s *Server) handleCafeFishSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	items, err := s.db.CafeFishSearch(r.Context(), q, 50)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"items": items})
}

// handleCafeSlotMixSet POST 一次设置三类槽位数量；总和必须为 10。
func (s *Server) handleCafeSlotMixSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Trophy   int `json:"trophy"`
		Longterm int `json:"longterm"`
		Normal   int `json:"normal"`
	}
	if err := readJSON(r, &req); err != nil {
		httpError(w, 400, "参数错误")
		return
	}
	counts := map[string]int{"trophy": req.Trophy, "longterm": req.Longterm, "normal": req.Normal}
	if err := s.db.CafeSlotMixSetAll(r.Context(), counts); err != nil {
		httpError(w, 400, "三类槽位数量总和必须为 10，且各为 0~10")
		return
	}
	_ = s.db.AuditLogWrite(r.Context(), adminOf(r), "cafe_slot_mix", "",
		fmt.Sprintf("trophy=%d longterm=%d normal=%d", req.Trophy, req.Longterm, req.Normal))
	s.pushCafeReload(r)
	writeJSON(w, map[string]any{"ok": true})
}

// handleCafeSubsidySet POST 更新巨物重量级补贴区间。
func (s *Server) handleCafeSubsidySet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Grp      string `json:"grp"`
		MinBlueG int64  `json:"min_blue_g"`
		Lo       int64  `json:"lo_cents"`
		Hi       int64  `json:"hi_cents"`
	}
	if err := readJSON(r, &req); err != nil || req.Grp == "" {
		httpError(w, 400, "参数错误")
		return
	}
	if err := s.db.CafeGiantSubsidySet(r.Context(), req.Grp, req.MinBlueG, req.Lo, req.Hi); err != nil {
		httpError(w, 400, err.Error())
		return
	}
	_ = s.db.AuditLogWrite(r.Context(), adminOf(r), "cafe_subsidy", req.Grp,
		fmt.Sprintf("min=%d lo=%d hi=%d", req.MinBlueG, req.Lo, req.Hi))
	s.pushCafeReload(r)
	writeJSON(w, map[string]any{"ok": true})
}

// handleCafeQuantitySet POST 更新一个交单数量档位（普通鱼/达标鱼/稀有普通/稀有达标）。
func (s *Server) handleCafeQuantitySet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QKey     string `json:"qkey"`
		Quantity int64  `json:"quantity"`
	}
	if err := readJSON(r, &req); err != nil || req.QKey == "" {
		httpError(w, 400, "参数错误")
		return
	}
	valid := map[string]bool{"common_normal": true, "common_qualified": true,
		"rare_normal": true, "rare_qualified": true}
	if !valid[req.QKey] {
		httpError(w, 400, "未知档位: "+req.QKey)
		return
	}
	if err := s.db.CafeOrderQuantitySet(r.Context(), req.QKey, req.Quantity); err != nil {
		httpError(w, 400, err.Error())
		return
	}
	_ = s.db.AuditLogWrite(r.Context(), adminOf(r), "cafe_quantity", req.QKey,
		fmt.Sprintf("quantity=%d", req.Quantity))
	s.pushCafeReload(r)
	writeJSON(w, map[string]any{"ok": true})
}

// handleCafePeriodSet POST 更新一类槽位的轮换周期（秒）。
func (s *Server) handleCafePeriodSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind       string `json:"kind"`
		MinSeconds int64  `json:"min_seconds"`
		MaxSeconds int64  `json:"max_seconds"`
	}
	if err := readJSON(r, &req); err != nil || req.Kind == "" {
		httpError(w, 400, "参数错误")
		return
	}
	valid := map[string]bool{"trophy": true, "longterm": true, "normal": true}
	if !valid[req.Kind] {
		httpError(w, 400, "未知槽位: "+req.Kind)
		return
	}
	if err := s.db.CafeSlotPeriodSet(r.Context(), req.Kind, req.MinSeconds, req.MaxSeconds); err != nil {
		httpError(w, 400, err.Error())
		return
	}
	_ = s.db.AuditLogWrite(r.Context(), adminOf(r), "cafe_period", req.Kind,
		fmt.Sprintf("min=%d max=%d", req.MinSeconds, req.MaxSeconds))
	s.pushCafeReload(r)
	writeJSON(w, map[string]any{"ok": true})
}

// handleCafePool GET 候选池预览（全部或指定水域），随 query level 过滤。
func (s *Server) handleCafePool(w http.ResponseWriter, r *http.Request) {
	level := strings.TrimSpace(r.URL.Query().Get("level"))
	var rows []store.CafeBountyPoolRow
	var err error
	if level == "" {
		rows, err = s.db.CafeBountyPoolAll(r.Context())
	} else {
		rows, err = s.db.CafeBountyPoolByLevel(r.Context(), level)
	}
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	levels, _ := s.db.CafePoolLevels(r.Context())
	writeJSON(w, map[string]any{"rows": rows, "levels": levels})
}

// handleCafePoolRegenerate POST 通知 gameserver 重建候选池（异步，通过共享队列）。
func (s *Server) handleCafePoolRegenerate(w http.ResponseWriter, r *http.Request) {
	s.pushCafeReload(r)
	_ = s.db.AuditLogWrite(r.Context(), adminOf(r), "cafe_pool_regenerate", "", "")
	writeJSON(w, map[string]any{"ok": true, "queued": true})
}

// pushCafeReload 写入重载指令；gameserver 消费后重建候选池。
func (s *Server) pushCafeReload(r *http.Request) {
	_ = s.db.QueuePush(r.Context(), mustJSON(map[string]any{"op": "reload_cafe_config"}))
}

// handleCafeOrdersClear 清空所有玩家的当前咖啡厅订单并立刻重建候选池。
// 通过管理队列交给 gameserver 同步执行：清空订单表 → 重建候选池，
// 玩家下次打开咖啡厅即为全新订单（无需等订单自然过期）。
func (s *Server) handleCafeOrdersClear(w http.ResponseWriter, r *http.Request) {
	_ = s.db.QueuePush(r.Context(), mustJSON(map[string]any{"op": "clear_cafe_orders"}))
	_ = s.db.AuditLogWrite(r.Context(), adminOf(r), "cafe_orders_clear", "", "")
	writeJSON(w, map[string]any{"ok": true})
}
