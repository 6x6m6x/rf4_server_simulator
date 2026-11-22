package gateway

import (
	"context"
	"crypto/sha1"
	"crypto/tls"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"rf4server/internal/config"
	"rf4server/internal/metrics"
	"rf4server/internal/store"
)

func TestLoginPasswordCheck(t *testing.T) {
	// 包级计数器在 NewLoginServer 之外需要显式初始化。
	reg := prometheus.NewRegistry()
	loginRequests = metrics.NewCounter(reg, "gateway_login_requests_total", "")
	loginIssued = metrics.NewCounter(reg, "gateway_tokens_issued_total", "")
	loginErrors = metrics.NewCounter(reg, "gateway_login_errors_total", "")

	db, err := store.Open(filepath.Join(t.TempDir(), "login.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uid := int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.ExecContext(ctx, `UPDATE profile SET name='gw_tester' WHERE uid=?`, uid); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Gateway{
		AdvertiseHost: "127.0.0.1", AdvertisePort: 9341, Region: "CN",
		ServerName: "test", TokenTTL: 1, UseridStart: 1000001,
	}
	s := &LoginServer{cfg: cfg, store: NewSQLiteStore(db, log), log: log}
	s.handler = http.HandlerFunc(s.handleLogin)

	call := func(target string, secure bool) string {
		r := httptest.NewRequest("GET", target, nil)
		r.RemoteAddr = "127.0.0.1:51000"
		if secure {
			r.TLS = &tls.ConnectionState{}
		}
		w := httptest.NewRecorder()
		s.handler.ServeHTTP(w, r)
		return w.Body.String()
	}

	sum := sha1.Sum([]byte("pw123"))
	hash := hex.EncodeToString(sum[:])

	// 无凭据请求：照旧签发成功 XML。
	if body := call("/login.php", true); !strings.Contains(body, `<logon success="true">`) {
		t.Fatalf("无凭据应放行: %s", body)
	}
	// 玩家免密：带凭据也放行。
	if body := call("/login.php?login=gw_tester&password="+hash, true); !strings.Contains(body, `<logon success="true">`) {
		t.Fatalf("免密应放行: %s", body)
	}
	// 设置密码后错误密码被拒（HTTPS 失败格式）。
	if err := db.SetPassword(ctx, uid, hash); err != nil {
		t.Fatal(err)
	}
	if body := call("/login.php?login=gw_tester&password=bad", true); !strings.Contains(body, `<logon success="false"></logon>`) {
		t.Fatalf("错误密码应拒绝: %s", body)
	}
	// 明文兼容口失败格式为 <response>。
	if body := call("/login.php?login=gw_tester&password=bad", false); !strings.Contains(body, `<response success="false"></response>`) {
		t.Fatalf("明文口失败格式异常: %s", body)
	}
	// 正确密码放行。
	if body := call("/login.php?login=gw_tester&password="+hash, true); !strings.Contains(body, `<logon success="true">`) {
		t.Fatalf("正确密码应放行: %s", body)
	}
}
