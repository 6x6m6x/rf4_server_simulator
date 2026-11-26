package portal

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"rf4server/internal/store"
)

// 45 万银币（=4500 万银分）兑换 1 份（200 万银分 → 100 金分）必须成功。
func TestExchangeSilverEndpoint(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "ex.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(); err != nil { t.Fatal(err) }
	if _, err := db.EnsureProfile(context.Background(), 42); err != nil { t.Fatal(err) }
	// 45 万银币 = 45,000,000 银分；金币 0
	if _, err := db.SQL.Exec(`UPDATE profile SET silver=45000000,gold=0 WHERE uid=42`); err != nil { t.Fatal(err) }
	s := New(db, func(remote string) int64 { return 42 }, "")
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://game.test"+path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.1:1000"
		if token != "" { r.Header.Set("Authorization", "Bearer "+token) }
		w := httptest.NewRecorder(); s.ServeHTTP(w, r); return w
	}
	w := call("POST", "/portal/api/session", "", "{}")
	var login struct{ Token string `json:"token"` }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &login) != nil { t.Fatalf("登录失败: %d %s", w.Code, w.Body.String()) }
	w = call("POST", "/portal/api/exchange", login.Token, `{"units":1,"requestID":"ex-0001"}`)
	t.Logf("兑换响应 %d: %s", w.Code, w.Body.String())
	if w.Code != 200 { t.Fatalf("兑换失败: %s", w.Body.String()) }
	p, _ := db.GetProfile(context.Background(), 42)
	if p.Gold != 100 || p.Silver != 43000000 { t.Fatalf("兑换后 gold=%d silver=%d 期望100/43000000", p.Gold, p.Silver) }
}
