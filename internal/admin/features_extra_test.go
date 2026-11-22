package admin

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
	"rf4server/internal/store"
)

// newExtraTestServer 构造带 JWT 的管理后台测试服务器。
func newExtraTestServer(t *testing.T) (*Server, string, *store.DB) {
	t.Helper()
	db := testDB(t)
	_ = db.EnsureAdmin(context.Background(), "admin", "admin123")
	srv := NewServer(config.Admin{HTTPAddr: "127.0.0.1:0", JWTSecret: "test"}, db, logging.Discard(), nil)
	r := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"admin","password":"admin123"}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	var lr struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &lr); err != nil || lr.Token == "" {
		t.Fatalf("管理登录失败: %s", w.Body.String())
	}
	return srv, lr.Token, db
}

func callExtra(t *testing.T, srv *Server, token, method, path, body string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func TestPlayerPasswordStoreAndAPI(t *testing.T) {
	srv, token, db := newExtraTestServer(t)
	ctx := context.Background()
	uid := int64(4242)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, `UPDATE profile SET name='pw_tester' WHERE uid=?`, uid); err != nil {
		t.Fatal(err)
	}

	code, body := callExtra(t, srv, token, "POST", "/api/players/password", `{"uid":4242,"password":"secret"}`)
	if code != 200 {
		t.Fatalf("设置密码失败: %d %s", code, body)
	}
	sum := sha1.Sum([]byte("secret"))
	want := hex.EncodeToString(sum[:])
	got, err := db.GetPasswordSHA1(ctx, uid)
	if err != nil || got != want {
		t.Fatalf("密码哈希 = %q, want %q, err=%v", got, want, err)
	}
	// 密码与哈希都不回显。
	if strings.Contains(strings.ToLower(body), want) || strings.Contains(strings.ToLower(body), "secret") {
		t.Fatalf("响应泄漏了密码或哈希: %s", body)
	}
	found, err := db.FindUIDByName(ctx, "pw_tester")
	if err != nil || found != uid {
		t.Fatalf("按用户名查 uid = %d, want %d, err=%v", found, uid, err)
	}

	code, _ = callExtra(t, srv, token, "POST", "/api/players/password", `{"uid":4242,"password":""}`)
	if code != 200 {
		t.Fatalf("清空密码失败: %d", code)
	}
	got, _ = db.GetPasswordSHA1(ctx, uid)
	if got != "" {
		t.Fatalf("清空后密码应为空, got %q", got)
	}
}

func TestIPBindingsAPI(t *testing.T) {
	t.Chdir(t.TempDir()) // 与 gateway 一样使用工作目录下的 JSON 文件
	srv, token, _ := newExtraTestServer(t)

	code, body := callExtra(t, srv, token, "GET", "/api/players/ip-bindings", "")
	if code != 200 || !strings.Contains(body, `"bindings":[]`) {
		t.Fatalf("空绑定列表异常: %d %s", code, body)
	}

	code, body = callExtra(t, srv, token, "POST", "/api/players/ip-binding", `{"ip":"192.168.1.9","uid":555}`)
	if code != 200 {
		t.Fatalf("设置绑定失败: %d %s", code, body)
	}
	raw, err := os.ReadFile(ipBindingsFile)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]int64
	if err := json.Unmarshal(raw, &m); err != nil || m["192.168.1.9"] != 555 {
		t.Fatalf("绑定文件内容异常: %s, err=%v", raw, err)
	}
	code, body = callExtra(t, srv, token, "GET", "/api/players/ip-bindings", "")
	if code != 200 || !strings.Contains(body, `"ip":"192.168.1.9"`) || !strings.Contains(body, `"uid":555`) {
		t.Fatalf("绑定列表异常: %d %s", code, body)
	}

	code, _ = callExtra(t, srv, token, "POST", "/api/players/ip-binding", `{"ip":"192.168.1.9","uid":0}`)
	if code != 200 {
		t.Fatalf("删除绑定失败: %d", code)
	}
	raw, _ = os.ReadFile(ipBindingsFile)
	m = nil
	_ = json.Unmarshal(raw, &m)
	if _, ok := m["192.168.1.9"]; ok {
		t.Fatalf("绑定未删除: %s", raw)
	}
}

func TestPlayerDeleteAPI(t *testing.T) {
	srv, token, db := newExtraTestServer(t)
	ctx := context.Background()
	uid := int64(777)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO item(guid,uid,item_id) VALUES('extra_g1',777,'worm')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO mail(uid,text) VALUES(777,'hi')`); err != nil {
		t.Fatal(err)
	}

	code, _ := callExtra(t, srv, token, "POST", "/api/players/delete", `{"uid":777,"confirm":false}`)
	if code != 400 {
		t.Fatalf("未确认应拒绝: %d", code)
	}
	code, body := callExtra(t, srv, token, "POST", "/api/players/delete", `{"uid":777,"confirm":true}`)
	if code != 200 {
		t.Fatalf("删除失败: %d %s", code, body)
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM profile WHERE uid=777`,
		`SELECT COUNT(*) FROM item WHERE uid=777`,
		`SELECT COUNT(*) FROM mail WHERE uid=777`,
	} {
		var n int
		if err := db.SQL.QueryRowContext(ctx, q).Scan(&n); err != nil || n != 0 {
			t.Fatalf("残留数据: %s -> %d, err=%v", q, n, err)
		}
	}
}

func TestMailTemplateAndBroadcastAPI(t *testing.T) {
	srv, token, db := newExtraTestServer(t)
	ctx := context.Background()

	code, body := callExtra(t, srv, token, "POST", "/api/mail/template/save",
		`{"name":"周补偿","subject":"补偿","body":"抱歉","attachments_json":"[]"}`)
	if code != 200 {
		t.Fatalf("保存模板失败: %d %s", code, body)
	}
	var sr struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal([]byte(body), &sr)
	if sr.ID <= 0 {
		t.Fatalf("模板 id 异常: %s", body)
	}
	code, body = callExtra(t, srv, token, "GET", "/api/mail/templates", "")
	if code != 200 || !strings.Contains(body, "周补偿") {
		t.Fatalf("模板列表异常: %d %s", code, body)
	}

	code, body = callExtra(t, srv, token, "POST", "/api/mail/send", `{"uids":[11,12,11],"text":"hi"}`)
	if code != 200 || !strings.Contains(body, `"queued":2`) {
		t.Fatalf("群发应去重为 2 封: %d %s", code, body)
	}
	raw1, ok1, _ := db.QueuePop(ctx)
	raw2, ok2, _ := db.QueuePop(ctx)
	if !ok1 || !ok2 || !strings.Contains(raw1, `"uid":11`) || !strings.Contains(raw2, `"uid":12`) {
		t.Fatalf("队列消息异常: %s / %s", raw1, raw2)
	}
	// 兼容旧的单 uid 参数。
	code, body = callExtra(t, srv, token, "POST", "/api/mail/send", `{"uid":99,"text":"hi"}`)
	if code != 200 || !strings.Contains(body, `"queued":1`) {
		t.Fatalf("单 uid 兼容失败: %d %s", code, body)
	}

	code, _ = callExtra(t, srv, token, "POST", "/api/mail/template/delete",
		`{"id":`+jsonNumber(sr.ID)+`}`)
	if code != 200 {
		t.Fatalf("删除模板失败: %d", code)
	}
	code, body = callExtra(t, srv, token, "GET", "/api/mail/templates", "")
	if code != 200 || strings.Contains(body, "周补偿") {
		t.Fatalf("模板未删除: %d %s", code, body)
	}
}

func jsonNumber(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestAvatarVisibleInPlayerList(t *testing.T) {
	srv, token, db := newExtraTestServer(t)
	ctx := context.Background()
	if _, err := db.EnsureProfile(ctx, 888); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx,
		`UPDATE profile SET avatar='http://x/a.png' WHERE uid=888`); err != nil {
		t.Fatal(err)
	}
	code, body := callExtra(t, srv, token, "GET", "/api/players", "")
	if code != 200 || !strings.Contains(body, `"avatar":"http://x/a.png"`) {
		t.Fatalf("玩家列表应下发头像: %d %s", code, body)
	}
}
