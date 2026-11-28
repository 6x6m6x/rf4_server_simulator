package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"rf4server/internal/config"
	"rf4server/internal/logging"
)

// TestRewardTrophiesAndCardsAPI：奖杯清单/按需发放/一键卡牌三个路由。
func TestRewardTrophiesAndCardsAPI(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.EnsureAdmin(ctx, "admin", "admin123"); err != nil { t.Fatal(err) }
	if _, err := db.EnsureProfile(ctx, 1000001); err != nil { t.Fatal(err) }
	// 奖杯清单需要 item_def category=72；按需发放要校验 category。
	// 一条有中文名，一条没登记名字（验证回退 item_id 与默认全量展示）。
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO item_def(numeric_id,item_id,category,attrs_json) VALUES(9002,'statuette_fisherman_15',72,'{}')`); err != nil { t.Fatal(err) }
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO item_catalog(item_id,name,name_zh,kind,numeric_id) VALUES('statuette_fisherman_15','15条稀有鱼','15条稀有鱼','',9002)`); err != nil { t.Fatal(err) }
	if _, err := db.SQL.Exec(`INSERT OR REPLACE INTO item_def(numeric_id,item_id,category,attrs_json) VALUES(9003,'trophy_level_test',72,'{}')`); err != nil { t.Fatal(err) }
	cfg := config.Admin{HTTPAddr: "127.0.0.1:0", JWTSecret: "test"}
	srv := NewServer(cfg, db, logging.Discard(), WebFS())
	if _, err := srv.Start(); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	base := "http://" + srv.Addr()
	client := &http.Client{Timeout: 5 * time.Second}
	call := func(method, path, token, body string) (int, string) {
		t.Helper()
		r, _ := http.NewRequest(method, base+path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" { r.Header.Set("Authorization", "Bearer "+token) }
		resp, err := client.Do(r)
		if err != nil { t.Fatal(err) }
		defer resp.Body.Close()
		var out bytes.Buffer
		_, _ = out.ReadFrom(resp.Body)
		return resp.StatusCode, out.String()
	}
	code, body := call("POST", "/api/login", "", `{"username":"admin","password":"admin123"}`)
	if code != 200 { t.Fatalf("login: %d %s", code, body) }
	var login struct{ Token string `json:"token"` }
	_ = json.Unmarshal([]byte(body), &login)

	code, body = call("GET", "/api/reward-trophies", login.Token, "")
	if code != 200 { t.Fatalf("catalog: %d %s", code, body) }
	type trophyRow struct {
		ItemID string `json:"item_id"`
		Name   string `json:"name"`
		NameZh string `json:"name_zh"`
		NameEn string `json:"name_en"`
		Group  string `json:"group"`
	}
	var cat struct{ Items []trophyRow `json:"items"` }
	if err := json.Unmarshal([]byte(body), &cat); err != nil || len(cat.Items) != 2 { t.Fatalf("catalog 应默认返回全部 2 条: %s err=%v", body, err) }
	byID := map[string]trophyRow{}
	for _, it := range cat.Items { byID[it.ItemID] = it }
	if byID["statuette_fisherman_15"].NameZh != "15条稀有鱼" || byID["statuette_fisherman_15"].Name != "15条稀有鱼" {
		t.Fatalf("有中文名的奖杯应按中文名展示与搜索: %+v", byID["statuette_fisherman_15"])
	}
	if byID["trophy_level_test"].Name != "trophy_level_test" || byID["trophy_level_test"].NameZh != "" {
		t.Fatalf("无中文名的奖杯应回退 item_id: %+v", byID["trophy_level_test"])
	}
	if byID["statuette_fisherman_15"].Group != "稀有鱼奖座" || byID["trophy_level_test"].Group != "水域奖杯" {
		t.Fatalf("分组错误: %+v", cat.Items)
	}
	code, body = call("POST", "/api/players/reward-trophies", login.Token, `{"uid":1000001,"item_ids":["statuette_fisherman_15","worm_01"]}`)
	if code != 200 { t.Fatalf("grant trophies: %d %s", code, body) }
	code, body = call("POST", "/api/players/cards/grant-all", login.Token, `{"uid":1000001}`)
	if code != 200 { t.Fatalf("grant cards: %d %s", code, body) }
	// 鉴权检查
	if code, _ = call("GET", "/api/reward-trophies", "", ""); code != 401 { t.Fatalf("未登录应 401，实得 %d", code) }
}
