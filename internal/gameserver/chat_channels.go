package gameserver

// 24/5 频道列表按官服实包还原（2026-11-25 抓包样本，18160 项）。
// 数据以 gzip JSON 内嵌；按关键字过滤，主频道置顶。
// 同时提供频道隔离广播与空私人频道回收所需的辅助函数。

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

//go:embed assets/chat_channels.json.gz
var officialChatChannelsGZ []byte

type officialChatChannel struct {
	Key    string  `json:"k"`
	Name   string  `json:"n"`
	Region string  `json:"r"`
	Users  uint32  `json:"u"`
	Kind   uint32  `json:"y"`
	ID     uint32  `json:"i"`
	Priv   uint32  `json:"p"`
	Members []uint32 `json:"m"`
}

var officialChatChannels struct {
	once sync.Once
	all  []officialChatChannel
}

func loadOfficialChatChannels(log *slog.Logger) []officialChatChannel {
	officialChatChannels.once.Do(func() {
		zr, err := gzip.NewReader(bytes.NewReader(officialChatChannelsGZ))
		if err != nil {
			if log != nil { log.Warn("官服频道快照解压失败", "err", err) }
			return
		}
		defer zr.Close()
		if err := json.NewDecoder(zr).Decode(&officialChatChannels.all); err != nil && log != nil {
			log.Warn("官服频道快照解析失败", "err", err)
		}
	})
	return officialChatChannels.all
}

func chatChannelMatches(c officialChatChannel, keyword string) bool {
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw == "" || kw == " " { return true }
	return strings.Contains(strings.ToLower(c.Name), kw) || strings.Contains(strings.ToLower(c.Key), kw)
}

// buildOfficialChannelObject 构造官服 obj402 godhiicbgng。
func buildOfficialChannelObject(c officialChatChannel, users uint32) *proto.Value {
	return wiregen.MustMakeObj("godhiicbgng", map[int]*proto.Value{
		0: proto.StrVal(c.Key),
		1: proto.StrVal(c.Name),
		2: proto.StrVal(c.Region),
		3: proto.U32(users),
		4: proto.U32(c.Kind),
		5: proto.U32(0),
		6: proto.U32Arr(c.Members),
		7: proto.U32(c.ID),
		8: proto.U32(c.Priv),
	})
}

// hSocialChatList 处理 24/5：按官服实包返回频道列表，主频道置顶。
func hSocialChatList(ctx *Ctx) ([]byte, bool) {
	strs := argStringsTop(ctx.Tree)
	keyword := ""
	if len(strs) > 0 { keyword = strs[0] }
	all := loadOfficialChatChannels(nil)
	items := make([]*proto.Value, 0, 256)
	// 主频道（y==0）优先
	for pass := 0; pass < 2; pass++ {
		for _, c := range all {
			isMain := c.Kind == 0
			if (pass == 0) != isMain { continue }
			if !chatChannelMatches(c, keyword) { continue }
			users := c.Users
			if ctx.Sess != nil && ctx.Sess.Srv != nil && isMain {
				users = uint32(ctx.Sess.Srv.onlineSessionCount())
			}
			items = append(items, buildOfficialChannelObject(c, users))
			if len(items) >= 512 { break }
		}
	}
	if len(items) == 0 { return emptyArgsBody(), true }
	body, err := proto.EncodeValue(proto.Args(proto.S("137"), items...), ctx.Sess.Table())
	if err != nil { return nil, false }
	return body, true
}

// channelPeers 返回指定频道在线会话快照（公聊广播与人数统计共用）。
func (s *Server) channelPeers(channelID string) []*Session {
	if s == nil { return nil }
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Session, 0, 8)
	for _, sess := range s.sessions {
		if sess == nil || sess.Closed() { continue }
		if sessChannel(sess).Name == channelID { out = append(out, sess) }
	}
	return out
}

// channelOnlineCount 频道在线人数（按会话计，不补 1）。
func (s *Server) channelOnlineCount(channelID string) int64 {
	return int64(len(s.channelPeers(channelID)))
}

// onlineSessionCount 全服在线会话数（主频道人数）。
func (s *Server) onlineSessionCount() int64 {
	if s == nil { return 0 }
	s.mu.Lock(); defer s.mu.Unlock()
	var n int64
	for _, sess := range s.sessions { if sess != nil && !sess.Closed() { n++ } }
	return n
}

// sweepEmptyPrivateChannels 回收无人在线的自建私人频道。
// 当前私人频道注册表在 misc_fix 中按会话状态维护，无持久行可删；保留空实现
// 以保证切频道/断开路径调用安全。
func (s *Server) sweepEmptyPrivateChannels() {}

// hSocialCreateChannel 处理 24/6：创建私人频道。
func hSocialCreateChannel(ctx *Ctx) ([]byte, bool) {
	name := argFirstStr(ctx.Tree)
	if name == "" { return emptyArgsBody(), true }
	ctx.Sess.SetState("channel", map[string]any{
		"name": name, "region": "CN", "users": 1, "private": true,
	})
	return []byte{0x00}, true
}

// hSocialJoinChannel 处理 24/7：加入频道（按 GUID/显示名）。
func hSocialJoinChannel(ctx *Ctx) ([]byte, bool) {
	name := argFirstStr(ctx.Tree)
	if name == "" { name = sessChannel(ctx.Sess).Name }
	ctx.Sess.SetState("channel", map[string]any{
		"name": name, "region": "CN", "users": 1, "private": false,
	})
	return []byte{0x00}, true
}
