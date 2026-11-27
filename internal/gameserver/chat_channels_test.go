package gameserver

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// 聊天频道体系的回归测试。根因与方案见
// specs/changes/chat-channel-fix/spec.md。

// newChatTestServer 造一个带会话表的 Server。
func newChatTestServer(t *testing.T) *Server {
	t.Helper()
	if gsPushes == nil {
		initMetrics(prometheus.NewRegistry())
	}
	srv := &Server{
		sessions:     map[uint64]*Session{},
		byToken:      map[string]*Session{},
		chatChannels: newChatChannelRegistry(),
		table:        wiregen.Table(),
		log:          logging.Discard(),
		callSeq:      map[string]uint64{},
	}
	return srv
}

// addChatSession 往 Server 里加一个在线会话。
func addChatSession(t *testing.T, srv *Server, sid uint64, uid int64, channel string) *Session {
	t.Helper()
	sess := &Session{ID: sid, Userid: uid, Srv: srv, log: logging.Discard(), stopCh: make(chan struct{})}
	sess.state = map[string]any{}
	if channel != "" {
		sess.SetState("channel", map[string]any{
			"name": channel, "region": "CN", "users": int64(1), "private": false,
		})
	}
	srv.mu.Lock()
	srv.sessions[sid] = sess
	srv.mu.Unlock()
	return sess
}

// fieldU32 读数值字段。
func fieldU32(f proto.Field) uint64 {
	if f.Num == nil {
		return 0
	}
	return *f.Num
}

// fieldStr 读字符串字段。
func fieldStr(f proto.Field) string {
	if f.Str == nil {
		return ""
	}
	return *f.Str
}

// decodeChannelObject 解出 402 频道对象的各字段。
func decodeChannelObject(t *testing.T, body []byte) []proto.Field {
	t.Helper()
	v, _, err := proto.DecodeValue(body, 0, wiregen.Table())
	if err != nil {
		t.Fatalf("402 解码失败: %v", err)
	}
	if v.Kind != proto.KindObj {
		t.Fatalf("402 应为对象，实得 kind=%v", v.Kind)
	}
	return v.Fields
}

// 频道人数必须按频道分组统计，而不是全服在线数。
func TestChannelOnlineCountPerChannel(t *testing.T) {
	srv := newChatTestServer(t)
	addChatSession(t, srv, 1, 1000001, "nav1_03")
	addChatSession(t, srv, 2, 1000002, "nav1_03")
	addChatSession(t, srv, 3, 1000003, "nav12_02")

	if n := srv.channelOnlineCount("nav1_03"); n != 2 {
		t.Fatalf("nav1_03 频道在线数应为 2，实得 %d", n)
	}
	if n := srv.channelOnlineCount("nav12_02"); n != 1 {
		t.Fatalf("nav12_02 频道在线数应为 1，实得 %d", n)
	}
	if n := srv.channelOnlineCount("nav9_99"); n != 0 {
		t.Fatalf("无人频道的真实人数应为 0（占位 1 会阻止空频道回收），实得 %d", n)
	}
}

// 同频道才互为 peer：公聊广播与人数统计共用这一口径，
// 防止不同主频道/私人频道之间串聊。
func TestChannelPeersOnlySameChannel(t *testing.T) {
	srv := newChatTestServer(t)
	a := addChatSession(t, srv, 1, 1000001, "nav19_01")
	addChatSession(t, srv, 2, 1000002, "nav19_01")
	addChatSession(t, srv, 3, 1000003, "nav19_03")
	addChatSession(t, srv, 4, 1000004, featuredPrivateGUID)

	peers := srv.channelPeers(sessChannel(a).Name)
	if len(peers) != 2 {
		t.Fatalf("nav19_01 应只有 2 个 peer，实得 %d", len(peers))
	}
	for _, p := range peers {
		if p.ID != 1 && p.ID != 2 {
			t.Fatalf("不同频道会话 %d 被当成 peer", p.ID)
		}
	}
	if got := srv.channelOnlineCount(featuredPrivateGUID); got != 1 {
		t.Fatalf("梅利号真实在线数应为 1，实得 %d", got)
	}
}

// 24/3 的 402：f3 必须是频道人数，不是全服在线数；f4 是频道可见性。
func TestSocialCurrentReturnsChannelScopedUsers(t *testing.T) {
	srv := newChatTestServer(t)
	sess := addChatSession(t, srv, 1, 1000001, "nav1_03")
	addChatSession(t, srv, 2, 1000002, "nav1_03")
	addChatSession(t, srv, 3, 1000003, "nav12_02")

	body, ok := hSocialCurrent(miscCtx(sess, 24, 3, proto.Args524()))
	if !ok {
		t.Fatal("24/3 未处理")
	}
	fields := decodeChannelObject(t, body)
	if len(fields) < 9 {
		t.Fatalf("402 字段数不足: %d", len(fields))
	}
	users := fieldU32(fields[3])
	if users != 2 {
		t.Fatalf("402.f3 应为本频道在线数 2，实得 %d（全服 3 人是错的口径）", users)
	}
	if fieldU32(fields[4]) != uint64(chatChannelPublic) {
		t.Fatalf("402.f4 主频道应为 Public(0)，实得 %d", fieldU32(fields[4]))
	}
}

// 个人频道：f4 必须是 Private(1)，显示名不能硬编码成"公共频道"。
func TestSocialCurrentReportsPrivateChannel(t *testing.T) {
	srv := newChatTestServer(t)
	sess := addChatSession(t, srv, 1, 1000001, "")
	sess.SetState("channel", map[string]any{
		"name": "myroom", "display": "我的房间", "region": "CN",
		"users": int64(1), "private": true, "channelID": uint32(777),
	})
	body, ok := hSocialCurrent(miscCtx(sess, 24, 3, proto.Args524()))
	if !ok {
		t.Fatal("24/3 未处理")
	}
	fields := decodeChannelObject(t, body)
	if fieldU32(fields[4]) != uint64(chatChannelPrivate) {
		t.Fatalf("个人频道 402.f4 应为 Private(1)，实得 %d", fieldU32(fields[4]))
	}
	if got := fieldStr(fields[1]); got != "我的房间" {
		t.Fatalf("402.f1 应为该频道显示名「我的房间」，实得 %q", got)
	}
	if got := fieldStr(fields[0]); got != "myroom" {
		t.Fatalf("402.f0 应为频道 id「myroom」，实得 %q", got)
	}
	if got := fieldU32(fields[7]); got != 777 {
		t.Fatalf("402.f7 频道数字 id 应为 777，实得 %d", got)
	}
}

// 24/6 create：创建个人频道并回 402（f4=Private），会话切到该频道。
func TestSocialCreatePersonalChannel(t *testing.T) {
	srv := newChatTestServer(t)
	sess := addChatSession(t, srv, 1, 1000001, "")
	body, ok := hSocialCreateChannel(miscCtx(sess, 24, 6,
		proto.Args524(proto.StrVal("myroom"), proto.StrVal("我的房间"))))
	if !ok {
		t.Fatal("24/6 未处理")
	}
	fields := decodeChannelObject(t, body)
	if fieldU32(fields[4]) != uint64(chatChannelPrivate) {
		t.Fatalf("24/6 创建的频道 f4 应为 Private(1)，实得 %d", fieldU32(fields[4]))
	}
	if got := fieldStr(fields[1]); got != "我的房间" {
		t.Fatalf("402.f1 应为请求里的显示名，实得 %q", got)
	}
	ch := sessChannel(sess)
	if ch.Name != "myroom" || !ch.Private {
		t.Fatalf("创建后会话频道应切到 myroom 且为 private，实得 %#v", ch)
	}
	// 频道应已登记，便于 24/7 join 认得它是 Private。
	srv.mu.Lock()
	registered := srv.chatChannels.get("myroom")
	srv.mu.Unlock()
	if registered == nil || registered.Kind != chatChannelPrivate {
		t.Fatalf("个人频道未登记或类型不对: %#v", registered)
	}
}

// 24/7 join：加入已登记的个人频道 → f4=Private；加入未登记频道 → 按 Public。
func TestSocialJoinChannel(t *testing.T) {
	srv := newChatTestServer(t)
	sess := addChatSession(t, srv, 1, 1000001, "")
	srv.mu.Lock()
	srv.chatChannels.put(&chatChannelInfo{
		ID: "myroom", Display: "我的房间", Region: "CN",
		Kind: chatChannelPrivate, ChannelID: 900, Members: map[int64]bool{},
	})
	srv.mu.Unlock()

	body, ok := hSocialJoinChannel(miscCtx(sess, 24, 7, proto.Args524(proto.StrVal("myroom"))))
	if !ok {
		t.Fatal("24/7 未处理")
	}
	fields := decodeChannelObject(t, body)
	if fieldU32(fields[4]) != uint64(chatChannelPrivate) {
		t.Fatalf("加入已登记个人频道 f4 应为 Private(1)，实得 %d", fieldU32(fields[4]))
	}
	if got := fieldStr(fields[1]); got != "我的房间" {
		t.Fatalf("402.f1 应为登记的显示名，实得 %q", got)
	}
	if ch := sessChannel(sess); ch.Name != "myroom" {
		t.Fatalf("加入后会话频道应为 myroom，实得 %q", ch.Name)
	}

	// 加入未登记的频道按主频道（Public）处理。
	sess2 := addChatSession(t, srv, 2, 1000002, "")
	body2, ok := hSocialJoinChannel(miscCtx(sess2, 24, 7, proto.Args524(proto.StrVal("nav3_01"))))
	if !ok {
		t.Fatal("24/7 未处理")
	}
	f2 := decodeChannelObject(t, body2)
	if fieldU32(f2[4]) != uint64(chatChannelPublic) {
		t.Fatalf("未登记频道 f4 应为 Public(0)，实得 %d", fieldU32(f2[4]))
	}
}

// 24/1 move：回该频道的 402（形状表要求 ancileefnje），会话频道同步更新。
// 这是"加入个人频道后无法退回主频道"的修复点。
func TestSocialMoveReturnsChannelObject(t *testing.T) {
	srv := newChatTestServer(t)
	sess := addChatSession(t, srv, 1, 1000001, "nav1_03")
	body, ok := hSocialMove(miscCtx(sess, 24, 1, proto.Args524(proto.StrVal("nav12_02"))))
	if !ok {
		t.Fatal("24/1 未处理")
	}
	fields := decodeChannelObject(t, body)
	if got := fieldStr(fields[0]); got != "nav12_02" {
		t.Fatalf("24/1 响应 402.f0 应为请求的频道 id，实得 %q", got)
	}
	if ch := sessChannel(sess); ch.Name != "nav12_02" {
		t.Fatalf("24/1 后会话频道应为 nav12_02，实得 %q", ch.Name)
	}
	// 退��主频道：再发一次 24/1，响应与状态都应回到主频道。
	body2, ok := hSocialMove(miscCtx(sess, 24, 1, proto.Args524(proto.StrVal("nav1_03"))))
	if !ok {
		t.Fatal("24/1 未处理")
	}
	f2 := decodeChannelObject(t, body2)
	if got := fieldStr(f2[0]); got != "nav1_03" {
		t.Fatalf("退回后 402.f0 应为主频道 id，实得 %q", got)
	}
	// 主频道 id 归一化后统一为快照口径 nav19_01[CN]，不再是历史默认名 nav1_03。
	if ch := sessChannel(sess); ch.Name != "nav19_01[CN]" || ch.Private {
		t.Fatalf("退回后应回到公共主频道（归一化 nav19_01[CN]），实得 %#v", ch)
	}
}

// 公聊「别人看不到」回归：同一中国频道存在两种 id 形态——客户端列表给的快照
// nav19_01[CN]，与从未切频道玩家的历史默认 nav1_03。channelPeers 必须把它们
// 归为同一频道，否则同频道玩家互相收不到发言（鱼获走全服广播所以不受影响）。
func TestChannelPeersNormalizesMainChannelIDForms(t *testing.T) {
	srv := newChatTestServer(t)
	addChatSession(t, srv, 1, 1000001, "nav1_03")
	addChatSession(t, srv, 2, 1000002, "nav19_01[CN]")
	addChatSession(t, srv, 3, 1000003, "nav19_01[GL]")

	if n := len(srv.channelPeers("nav1_03")); n != 2 {
		t.Fatalf("nav1_03 与 nav19_01[CN] 应视为同一中国频道，实得 %d", n)
	}
	if n := len(srv.channelPeers("nav19_01[CN]")); n != 2 {
		t.Fatalf("按快照 id 查询也应命中同一中国频道，实得 %d", n)
	}
	if n := srv.channelOnlineCount("nav19_01[CN]"); n != 2 {
		t.Fatalf("中国频道人数应为 2，实得 %d", n)
	}
	if n := len(srv.channelPeers("nav19_01[GL]")); n != 1 {
		t.Fatalf("国际频道应独立统计，实得 %d", n)
	}
}

// 主频道 id 归一化规则：所有「中国频道」的形态都收敛到快照 nav19_01[CN]。
func TestNormalizeChatChannelName(t *testing.T) {
	cases := map[string]string{
		"":              "nav19_01[CN]",
		"nav1_03":       "nav19_01[CN]",
		"nav1_03[CN]":   "nav19_01[CN]",
		"nav19_01":      "nav19_01[CN]",
		"nav19_01[CN]":  "nav19_01[CN]",
		"nav19_01[GL]":  "nav19_01[GL]",
		"nav12_02":      "nav12_02",
		"ade934f7-25ca-4f85-93d3-8d6e6c18d7b4": "ade934f7-25ca-4f85-93d3-8d6e6c18d7b4",
	}
	for in, want := range cases {
		if got := normalizeChatChannelName(in); got != want {
			t.Fatalf("normalize(%q) = %q; want %q", in, got, want)
		}
	}
}
