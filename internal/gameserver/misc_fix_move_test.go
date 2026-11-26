package gameserver

import (
	"bytes"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// 24/1 记录频道并回该频道的 402 频道对象（形状表 social/move → ancileefnje）。
//
// 此前回 null：客户端拿不到频道对象就不更新界面标签，表现为"切了频道界面不动"、
// "进了个人频道退不回主频道"。而"回 402 会导致 16ms 重发断开"的根因是**内容与
// 请求不匹配**（f1 硬编码"公共频道"、f4 恒 Public、f7 恒 1），不是"回 402"本身。
// 详见 specs/changes/chat-channel-fix/spec.md。
func TestSocialMoveReturnsChannelObjectAndRecordsChannel(t *testing.T) {
	srv := &Server{
		sessions:     map[uint64]*Session{},
		byToken:      map[string]*Session{},
		chatChannels: newChatChannelRegistry(),
		table:        wiregen.Table(),
		log:          logging.Discard(),
	}
	sess := newProbeSess(srv)
	sess.state = make(map[string]any)
	body, ok := hSocialMove(miscCtx(sess, 24, 1, proto.Args524(proto.StrVal("nav12_02"))))
	if !ok {
		t.Fatal("24/1 未处理")
	}
	if bytes.Equal(body, []byte{0x00}) {
		t.Fatal("24/1 不应再回 null")
	}
	decoded, _, err := proto.DecodeValue(body, 0, wiregen.Table())
	if err != nil {
		t.Fatalf("24/1 响应解码失败: %v", err)
	}
	if decoded.Kind != proto.KindObj {
		t.Fatalf("24/1 响应应为 402 频道对象，实得 kind=%v", decoded.Kind)
	}
	if got := fieldStr(decoded.Fields[0]); got != "nav12_02" {
		t.Fatalf("24/1 响应 402.f0 应为请求的频道 id，实得 %q", got)
	}
	v, exists := sess.GetState("channel")
	if !exists {
		t.Fatal("24/1 未记录会话频道")
	}
	m, isMap := v.(map[string]any)
	if !isMap || m["name"] != "nav12_02" {
		t.Fatalf("24/1 记录的频道=%#v", v)
	}
}
