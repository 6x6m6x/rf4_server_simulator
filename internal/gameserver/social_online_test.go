package gameserver

import (
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// 在线灯（24/19 资料卡 f6）的回归测试。
//
// 根因：2002 资料卡 dgbmlekfpbg 的 f6 字段语义是「在线（online）」，客户端据此
// 点亮/熄灭联系人列表里的在线灯。此前该字段恒传 0 —— 无论对方是否真的在线，
// 灯永远灭着。
//
// 本文件只依赖 HEAD 基线已有的辅助（miscCtx / decodeWrappedBytes），不依赖
// 聊天频道体系的任何东西 —— 这样「在线灯」可以独立成补丁应用。

// newOnlineTestServer 造一个带会话表的 Server（最小构造，无聊天频道表）。
func newOnlineTestServer(t *testing.T) *Server {
	t.Helper()
	return &Server{
		sessions: map[uint64]*Session{},
		byToken:  map[string]*Session{},
		table:    wiregen.Table(),
		log:      logging.Discard(),
	}
}

// addOnlineTestSession 往 Server 里加一个在线会话。
func addOnlineTestSession(t *testing.T, srv *Server, sid uint64, uid int64) *Session {
	t.Helper()
	sess := &Session{ID: sid, Userid: uid, Srv: srv, log: logging.Discard(), stopCh: make(chan struct{})}
	sess.state = map[string]any{}
	srv.mu.Lock()
	srv.sessions[sid] = sess
	srv.mu.Unlock()
	return sess
}

// onlineFieldU32 读资料卡的数值字段。
func onlineFieldU32(f proto.Field) uint64 {
	if f.Num == nil {
		return 0
	}
	return *f.Num
}

// 24/19 资料卡的 f6（在线）必须反映目标 uid 的真实在线状态。
func TestSocialUserCardOnlineFlag(t *testing.T) {
	srv := newOnlineTestServer(t)
	addOnlineTestSession(t, srv, 1, 1000002) // 对方，在线
	sess := addOnlineTestSession(t, srv, 2, 1000001)

	// 对方在线 → f6=1
	body, ok := hSocialUserCard(miscCtx(sess, 24, 19, proto.Args524(proto.U32(1000002))))
	if !ok {
		t.Fatal("24/19 未处理")
	}
	// 24/19 的响应是 bytes 包装的对象。
	outer, _, err := proto.DecodeValue(body, 0, wiregen.Table())
	if err != nil {
		t.Fatalf("24/19 解码失败: %v", err)
	}
	inner := decodeWrappedBytes(t, outer)
	if onlineFieldU32(inner.Fields[6]) != 1 {
		t.Fatalf("对方在线时 2002.f6 应为 1，实得 %d", onlineFieldU32(inner.Fields[6]))
	}

	// 对方离线 → f6=0
	body2, ok := hSocialUserCard(miscCtx(sess, 24, 19, proto.Args524(proto.U32(1999999))))
	if !ok {
		t.Fatal("24/19 未处理")
	}
	outer2, _, _ := proto.DecodeValue(body2, 0, wiregen.Table())
	inner2 := decodeWrappedBytes(t, outer2)
	if onlineFieldU32(inner2.Fields[6]) != 0 {
		t.Fatalf("对方离线时 2002.f6 应为 0，实得 %d", onlineFieldU32(inner2.Fields[6]))
	}
}

// 已关闭的会话不算在线 —— 玩家断线后灯必须灭。
func TestSocialUserCardOnlineFlagIgnoresClosedSession(t *testing.T) {
	srv := newOnlineTestServer(t)
	other := addOnlineTestSession(t, srv, 1, 1000002)
	sess := addOnlineTestSession(t, srv, 2, 1000001)

	// 模拟对方断线：会话被标记为 closed。
	other.closed.Store(true)

	body, ok := hSocialUserCard(miscCtx(sess, 24, 19, proto.Args524(proto.U32(1000002))))
	if !ok {
		t.Fatal("24/19 未处理")
	}
	outer, _, err := proto.DecodeValue(body, 0, wiregen.Table())
	if err != nil {
		t.Fatalf("24/19 解码失败: %v", err)
	}
	inner := decodeWrappedBytes(t, outer)
	if onlineFieldU32(inner.Fields[6]) != 0 {
		t.Fatalf("会话已关闭时 2002.f6 应为 0，实得 %d", onlineFieldU32(inner.Fields[6]))
	}
}

// userOnline 直接判定：会话存在且未关闭才算在线。
func TestUserOnlineOnlyCountsLiveSessions(t *testing.T) {
	srv := newOnlineTestServer(t)
	if srv.userOnline(0) {
		t.Fatal("uid=0 不应判为在线")
	}
	if srv.userOnline(1000002) {
		t.Fatal("没有会话时不应判为在线")
	}
	addOnlineTestSession(t, srv, 1, 1000002)
	if !srv.userOnline(1000002) {
		t.Fatal("有活跃会话时应判为在线")
	}
	srv.mu.Lock()
	for _, s := range srv.sessions {
		if s.Userid == 1000002 {
			s.closed.Store(true)
		}
	}
	srv.mu.Unlock()
	if srv.userOnline(1000002) {
		t.Fatal("会话已关闭后不应判为在线")
	}
}
