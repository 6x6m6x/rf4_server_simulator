package gameserver

import (
	"net"
	"strings"
	"testing"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/tunnel"
	"rf4server/internal/wiregen"
)

// 24/5 按官服实包还原：args(137) + obj402，主频道置顶显示本服在线人数，
// 私人频道含快照里可加入的"梅利号空军基地"。其余快照条目不填充到本服。
// 数据来源 logs/net_capture_channel_20261125.jsonl。

func decodeChatList(t *testing.T, body []byte) *proto.Value {
	t.Helper()
	v, _, err := proto.DecodeValue(body, 0, wiregen.Table())
	if err != nil {
		t.Fatalf("24/5 响应解码失败: %v", err)
	}
	if v.Kind != proto.KindArgs || v.ArgCodeStr() != "137" {
		t.Fatalf("24/5 应为 args(137)，实得 kind=%s code=%q", v.Kind, v.ArgCodeStr())
	}
	return v
}

func officialChannelField(t *testing.T, v *proto.Value, i int) (key, name, region string, users, kind, number uint32) {
	t.Helper()
	if v == nil || v.Kind != proto.KindObj || v.TypeID != 402 {
		t.Fatalf("第 %d 项应为 obj402，实得 kind=%s tid=%d", i, v.Kind, v.TypeID)
	}
	f0, f1, f2, f3, f4, f7 := v.Field(0), v.Field(1), v.Field(2), v.Field(3), v.Field(4), v.Field(7)
	return f0.StrOr(), f1.StrOr(), f2.StrOr(), f3.U32(), f4.U32(), f7.U32()
}

// 空搜索键：主频道（f4=0）置顶，人数=本服在线会话数；私人频道含梅利号。
func TestSocialChatListOfficialFullList(t *testing.T) {
	srv := newChatTestServer(t)
	addChatSession(t, srv, 1, 1000001, "nav1_03")
	addChatSession(t, srv, 2, 1000002, "nav1_03")
	addChatSession(t, srv, 3, 1000003, "nav12_02")

	body, ok := hSocialChatList(miscCtx(srv.sessions[1], 24, 5, proto.Args524(proto.StrVal(""), proto.StrVal("nav19_01"))))
	if !ok {
		t.Fatal("24/5 未处理")
	}
	list := decodeChatList(t, body)
	// 只列精选：2 主频道 + 1 预置私人频道（梅利号）。
	if got := list.ArgCount(); got < 3 || got > 8 {
		t.Fatalf("应只列精选频道（约 3 项），实得 %d", got)
	}
	key0, name0, region0, users0, kind0, _ := officialChannelField(t, list.ArgItem(0), 0)
	_, _, _, users1, kind1, _ := officialChannelField(t, list.ArgItem(1), 1)
	if kind0 != chatChannelPublic || kind1 != chatChannelPublic {
		t.Fatalf("前两项应为主频道 f4=0，实得 %d/%d", kind0, kind1)
	}
	if !strings.Contains(key0, "["+region0+"]") || name0 == "" {
		t.Fatalf("主频道 f0/f1/f2 形状不对: key=%q name=%q region=%q", key0, name0, region0)
	}
	// 主频道人数是本服在线会话数（3），不是快照官服在线数。
	if users0 != 3 || users1 != 3 {
		t.Fatalf("主频道人数应为本服在线 3，实得 %d/%d", users0, users1)
	}
	// 快照其余 18000+ 频道不得填充。
	if strings.Contains(string(body), "ZeroTem") || strings.Contains(string(body), "0000ba77") {
		t.Fatal("快照里的无关频道被填充到列表")
	}
}

// 预置私人频道（显示名"羊村"）必须出现在列表里，且用快照真实参数（GUID / 2251665 / GL）。
func TestSocialChatListHasMeiliPrivateChannel(t *testing.T) {
	srv := newChatTestServer(t)
	sess := addChatSession(t, srv, 1, 1000001, "nav1_03")

	body, ok := hSocialChatList(miscCtx(sess, 24, 5, proto.Args524(proto.StrVal("羊村"), proto.StrVal("nav19_01"))))
	if !ok {
		t.Fatal("24/5 未处理")
	}
	list := decodeChatList(t, body)
	found := false
	for i := 0; i < list.ArgCount(); i++ {
		key, name, region, users, kind, number := officialChannelField(t, list.ArgItem(i), i)
		if key == featuredPrivateGUID {
			if name != featuredPrivateName || region != "GL" || kind != chatChannelPrivate || number != 2251665 {
				t.Fatalf("羊村参数与快照不符: name=%q region=%q kind=%d num=%d", name, region, kind, number)
			}
			if users != 0 {
				t.Fatalf("羊村没人时人数应为 0，实得 %d（占位 1 会阻止空频道回收）", users)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("搜索「羊村」未命中预置私人频道，列表 %d 项", list.ArgCount())
	}
}

// 私人频道（羊村）可经 24/7 join 实际切换加入：回 Private 402，
// GUID/number 保持快照值，且会话切过去后不是 nav 主频道。
func TestSocialJoinVillagePrivateChannel(t *testing.T) {
	srv := newChatTestServer(t)
	sess := addChatSession(t, srv, 1, 1000001, "nav1_03")

	body, ok := hSocialJoinChannel(miscCtx(sess, 24, 7,
		proto.Args524(proto.StrVal(featuredPrivateGUID), proto.StrVal(featuredPrivateName))))
	if !ok {
		t.Fatal("24/7 未处理")
	}
	v, _, err := proto.DecodeValue(body, 0, wiregen.Table())
	if err != nil || v == nil || v.Kind != proto.KindObj {
		t.Fatalf("24/7 应回 obj402: err=%v v=%v", err, v)
	}
	key, name, _, _, kind, number := officialChannelField(t, v, 0)
	if key != featuredPrivateGUID || name != featuredPrivateName || kind != chatChannelPrivate || number != 2251665 {
		t.Fatalf("加入梅利号应回 Private 402 快照参数: key=%q name=%q kind=%d num=%d", key, name, kind, number)
	}
	ch := sessChannel(sess)
	if ch.Name != featuredPrivateGUID || !ch.Private || strings.HasPrefix(ch.Name, "nav") {
		t.Fatalf("会话应切到梅利号私人频道且非 nav 主频道: %#v", ch)
	}
	// 登记表里仍保持 Private。
	srv.mu.Lock()
	info := srv.chatChannels.get(featuredPrivateGUID)
	srv.mu.Unlock()
	if info == nil || info.Kind != chatChannelPrivate {
		t.Fatalf("梅利号登记类型应为 Private: %#v", info)
	}
}

// 24/1 move 也能切回主频道，再切到梅利号，主频道与个人频道互不冲突。
func TestSocialMoveBetweenMainAndMeili(t *testing.T) {
	srv := newChatTestServer(t)
	sess := addChatSession(t, srv, 1, 1000001, "nav1_03")

	// 切到梅利号。
	if _, ok := hSocialMove(miscCtx(sess, 24, 1, proto.Args524(proto.StrVal(featuredPrivateGUID)))); !ok {
		t.Fatal("24/1 切梅利号未处理")
	}
	if ch := sessChannel(sess); ch.Name != featuredPrivateGUID || !ch.Private {
		t.Fatalf("应切到梅利号私人频道: %#v", ch)
	}
	// 切回 nav 主频道。
	body, ok := hSocialMove(miscCtx(sess, 24, 1, proto.Args524(proto.StrVal("nav12_02"))))
	if !ok {
		t.Fatal("24/1 切回主频道未处理")
	}
	v, _, err := proto.DecodeValue(body, 0, wiregen.Table())
	if err != nil {
		t.Fatal(err)
	}
	key, _, _, _, kind, _ := officialChannelField(t, v, 0)
	if key != "nav12_02" || kind != chatChannelPublic {
		t.Fatalf("切回主频道应回 Public 402 nav12_02: key=%q kind=%d", key, kind)
	}
	if ch := sessChannel(sess); ch.Name != "nav12_02" || ch.Private {
		t.Fatalf("应回到公共主频道: %#v", ch)
	}
}

// 本服动态登记的个人频道也进入列表。
func TestSocialChatListIncludesLocalPrivateChannel(t *testing.T) {
	srv := newChatTestServer(t)
	sess := addChatSession(t, srv, 1, 1000001, "")
	srv.mu.Lock()
	srv.chatChannels.put(&chatChannelInfo{
		ID: "zz-local-room", Display: "本地测试频道", Region: "CN",
		Kind: chatChannelPrivate, ChannelID: 909, Members: map[int64]bool{},
	})
	srv.mu.Unlock()

	body, ok := hSocialChatList(miscCtx(sess, 24, 5, proto.Args524(proto.StrVal("本地测试"), proto.StrVal(""))))
	if !ok {
		t.Fatal("24/5 未处理")
	}
	list := decodeChatList(t, body)
	found := false
	for i := 2; i < list.ArgCount(); i++ {
		key, name, _, _, kind, _ := officialChannelField(t, list.ArgItem(i), i)
		if key == "zz-local-room" && name == "本地测试频道" && kind == chatChannelPrivate {
			found = true
		}
	}
	if !found {
		t.Fatal("本服登记的个人频道未出现在 24/5 列表")
	}
}

// 自建私人频道在最后一个会话离开（切频道或断线）后自动删除；
// 快照精选的梅利号保留，仍可加入。
func TestPrivateChannelAutoDeleteWhenEmpty(t *testing.T) {
	srv := newChatTestServer(t)
	sess := addChatSession(t, srv, 1, 1000001, "nav1_03")

	if _, ok := hSocialCreateChannel(miscCtx(sess, 24, 6,
		proto.Args524(proto.StrVal("gc-room"), proto.StrVal("自动回收房")))); !ok {
		t.Fatal("24/6 未处理")
	}
	if srv.chatChannels.get("gc-room") == nil {
		t.Fatal("创建后频道未登记")
	}
	if got := srv.channelOnlineCount("gc-room"); got != 1 {
		t.Fatalf("创建者应算 1 人在线，实得 %d", got)
	}

	// 切回主频道后，原私人频道无人，应立即回收。
	if _, ok := hSocialMove(miscCtx(sess, 24, 1, proto.Args524(proto.StrVal("nav12_02")))); !ok {
		t.Fatal("24/1 未处理")
	}
	if srv.chatChannels.get("gc-room") != nil {
		t.Fatal("切走自建私人频道后未自动删除")
	}
	if srv.chatChannels.get(featuredPrivateGUID) == nil {
		t.Fatal("空频道回收误删了常驻的梅利号")
	}

	// 再次创建后断线，同样要回收。
	if _, ok := hSocialCreateChannel(miscCtx(sess, 24, 6,
		proto.Args524(proto.StrVal("gc-room-2"), proto.StrVal("断线回收房")))); !ok {
		t.Fatal("24/6 未处理")
	}
	// 模拟断线：会话从在线表移除后触发回收（指标未初始化，不走 removeSession）。
	srv.mu.Lock()
	delete(srv.sessions, sess.ID)
	srv.mu.Unlock()
	srv.sweepEmptyPrivateChannels()
	if srv.chatChannels.get("gc-room-2") != nil {
		t.Fatal("会话断开后自建私人频道未自动删除")
	}
	if srv.chatChannels.get(featuredPrivateGUID) == nil {
		t.Fatal("断线回收误删了常驻的梅利号")
	}
}

// attachChatTunnel 给测试会话接一对内存隧道，客户端侧 Conn 用于读推送。
func attachChatTunnel(t *testing.T, sess *Session) *tunnel.Conn {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	type hs struct {
		conn *tunnel.Conn
		err  error
	}
	ch := make(chan hs, 1)
	go func() {
		c, err := tunnel.HandshakeServer(serverSide, "chat-test")
		ch <- hs{c, err}
	}()
	client, err := tunnel.HandshakeClient(clientSide, "chat-test")
	if err != nil {
		t.Fatal(err)
	}
	res := <-ch
	if res.err != nil {
		t.Fatal(res.err)
	}
	sess.tunnel = res.conn
	t.Cleanup(func() { _ = client.Close(); _ = res.conn.Close() })
	return client
}

// 24/10 公聊只推送给同频道会话：不同私人频道之间、私人频道与主频道之间
// 都不应互相看到。
func TestSocialPublicOnlyBroadcastsSameChannel(t *testing.T) {
	srv := newChatTestServer(t)
	setPrivateChannel := func(sess *Session, name string) {
		sess.SetState("channel", map[string]any{
			"name": name, "region": "GL", "users": int64(1),
			"private": true, "channelID": uint32(2251665),
		})
	}
	sender := addChatSession(t, srv, 1, 1000001, featuredPrivateGUID)
	sender.Token = "chat-sender"
	setPrivateChannel(sender, featuredPrivateGUID)
	same := addChatSession(t, srv, 2, 1000002, featuredPrivateGUID)
	same.Token = "chat-same"
	setPrivateChannel(same, featuredPrivateGUID)
	otherPrivate := addChatSession(t, srv, 3, 1000003, "other-private-room")
	otherPrivate.Token = "chat-other-private"
	setPrivateChannel(otherPrivate, "other-private-room")
	main := addChatSession(t, srv, 4, 1000004, "nav19_01")
	main.Token = "chat-main"

	cSender := attachChatTunnel(t, sender)
	cSame := attachChatTunnel(t, same)
	cOtherPrivate := attachChatTunnel(t, otherPrivate)
	cMain := attachChatTunnel(t, main)

	// net.Pipe 无缓冲，必须先挂上读取端再触发推送，否则 Push 会阻塞。
	beginRead := func(c *tunnel.Conn) <-chan bool {
		got := make(chan bool, 1)
		go func() {
			_, _, payload, err := c.ReadRecord()
			if err != nil {
				got <- false
				return
			}
			env, ok := proto.ParseEnvelope(payload)
			got <- ok && env.Main == 24 && env.Sub == 27
		}()
		return got
	}
	awaitChat := func(got <-chan bool, wait time.Duration) bool {
		select {
		case v := <-got:
			return v
		case <-time.After(wait):
			return false
		}
	}
	rSender, rSame, rOtherPrivate, rMain := beginRead(cSender), beginRead(cSame), beginRead(cOtherPrivate), beginRead(cMain)

	if _, ok := hSocialPublic(miscCtx(sender, 24, 10, proto.Args524(proto.StrVal("只给同频道")))); !ok {
		t.Fatal("24/10 未处理")
	}
	if !awaitChat(rSender, 3*time.Second) {
		t.Fatal("发送者自己未收到 24/27 回显")
	}
	if !awaitChat(rSame, 3*time.Second) {
		t.Fatal("同一私人频道的玩家未收到 24/27 回显")
	}
	if awaitChat(rOtherPrivate, 300*time.Millisecond) {
		t.Fatal("另一个私人频道收到了不该看到的公聊消息")
	}
	if awaitChat(rMain, 300*time.Millisecond) {
		t.Fatal("主频道收到了不该看到的私人频道消息")
	}
}

// 对象字段形状：9 字段、f6 为 VAL(u32arr/null)、f8 为 OPT，与官服 obj402 一致。
func TestSocialChatListObjectShape(t *testing.T) {
	mains, private := featuredChannels()
	if len(mains) == 0 || private == nil {
		t.Fatal("精选频道为空")
	}
	for _, c := range []officialChatChannel{mains[0], *private} {
		v := buildOfficialChannelObject(c, c.Users)
		fs := v.Fields
		if len(fs) != 9 {
			t.Fatalf("obj402 应 9 字段，实得 %d", len(fs))
		}
		if fs[0].Op != "STR" || fs[3].Op != "U32" || fs[4].Op != "BYTE" ||
			fs[6].Op != "VAL" || fs[7].Op != "U32" || fs[8].Op != "OPT" {
			t.Fatalf("obj402 字段 op 与实包不一致: %#v", fs)
		}
		if fs[6].Val == nil || (fs[6].Val.Kind != proto.KindNull && fs[6].Val.Kind != proto.KindU32Arr) {
			t.Fatalf("f6 应为 VAL(null) 或 VAL(u32arr)，实得 %#v", fs[6].Val)
		}
	}
}
