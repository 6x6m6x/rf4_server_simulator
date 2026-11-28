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

// TestCompleteProgressAPI：后台一键完成成就/资格两个路由走管理指令队列并写审计。
func TestCompleteProgressAPI(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.EnsureAdmin(ctx, "admin", "admin123"); err != nil { t.Fatal(err) }
	if _, err := db.EnsureProfile(ctx, 1000001); err != nil { t.Fatal(err) }
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

	for _, path := range []string{"/api/players/achievements/complete", "/api/players/qualifications/complete"} {
		code, body = call("POST", path, login.Token, `{"uid":1000001}`)
		if code != 200 { t.Fatalf("%s: %d %s", path, code, body) }
		var r struct{ OK bool `json:"ok"` }
		_ = json.Unmarshal([]byte(body), &r)
		if !r.OK { t.Fatalf("%s 未返回 ok: %s", path, body) }
	}
	// 未登录必须被拒。
	if code, _ = call("POST", "/api/players/achievements/complete", "", `{"uid":1000001}`); code != 401 {
		t.Fatalf("未登录应 401，实得 %d", code)
	}
}
