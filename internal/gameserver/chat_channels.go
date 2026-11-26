package gameserver

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

// 24/5 频道列表按官服实包还原：logs/net_capture_channel_20261125.jsonl 的
// 最大样本（args(137)，18160 项）逐字段提取成快照，gzip 后内嵌。
// 搜索空格键返回全部；关键字按频道显示名/频道 key 子串匹配；
// 主频道（f4=0）恒置顶，与实包一致。
//
//go:embed assets/chat_channels.json.gz
var officialChatChannelsGZ []byte

// officialChatChannel 是 obj 402 godhiicbgng 的紧凑快照字段。
type officialChatChannel struct {
	Key     string   `json:"k"`           // f0 频道 key（如 nav19_01[GL]）
	Name    string   `json:"n"`           // f1 显示名
	Region  string   `json:"r"`           // f2 区域
	Users   uint32   `json:"u"`           // f3 频道人数
	Kind    uint32   `json:"y"`           // f4 0=Public 1=Private
	Opt5    uint32   `json:"o"`           // f5 典型值 0/1
	Members []uint32 `json:"m,omitempty"` // f6 成员 uid 列表
	Number  uint32   `json:"i"`           // f7 频道数字 id
	Opt8    uint32   `json:"p"`           // f8 opt bool
}

var officialChatChannels struct {
	sync.Once
	list []officialChatChannel
	err  error
}

func loadOfficialChatChannels(log *slog.Logger) []officialChatChannel {
	officialChatChannels.Do(func() {
		zr, err := gzip.NewReader(bytes.NewReader(officialChatChannelsGZ))
		if err != nil {
			officialChatChannels.err = err
			return
		}
		defer zr.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(zr); err != nil {
			officialChatChannels.err = err
			return
		}
		if err := json.Unmarshal(buf.Bytes(), &officialChatChannels.list); err != nil {
		officialChatChannels.err = err
			return
		}
		if log != nil {
			log.Info("24/5 频道快照已加载", "channels", len(officialChatChannels.list))
		}
	})
	return officialChatChannels.list
}

// 本服只实现快照中的精选频道：2 个主频道 + "羊村"私人频道。
// 其余 18000+ 条只作为数据来源留存，不填充到本服列表。
//
// 羊村是快照里的常驻私人频道（沿用官服 GUID，仅改显示名）：人数按真实在线会话显示，但不会因无人
// 而被回收；本服 24/6 自建频道则在最后一个会话离开时自动删除。
const (
	featuredPrivateGUID = "ade934f7-25ca-4f85-93d3-8d6e6c18d7b4"
	// officialPrivateName 是官服快照里这条常驻私人频道的原名，用于匹配；
	// featuredPrivateName 是本服对外显示名（羊村）。
	officialPrivateName = "梅利号空军基地"
	featuredPrivateName = "羊村"
)

// featuredChannels 从快照挑出本服要实现的频道：主频道（f4=0）恒置顶，
// 以及按真实 GUID/名字匹配的精选私人频道（用快照原始参数）。
func featuredChannels() (mains []officialChatChannel, private *officialChatChannel) {
	for _, c := range loadOfficialChatChannels(nil) {
		if c.Kind == chatChannelPublic {
			mains = append(mains, c)
			continue
		}
		if c.Key == featuredPrivateGUID || c.Name == officialPrivateName || c.Name == featuredPrivateName {
			cp := c
			// 显示名统一为本服名（羊村），其余字段沿用快照。
			cp.Name = featuredPrivateName
			private = &cp
		}
	}
	return
}

// buildOfficialChannelObject 按官服实包字段值构造 obj 402。
// f6 成员列表为 null 时保持 VAL(null)，与官服从服务端拿到的形状一致。
func buildOfficialChannelObject(c officialChatChannel, users uint32) *proto.Value {
	// 人数必须如实反映在线会话数：私人频道 0 人时客户端才能按原逻辑回收。
	f6 := proto.Null()
	if len(c.Members) > 0 {
		f6 = proto.U32Arr(c.Members)
	}
	return wiregen.MustMakeObj("godhiicbgng", map[int]*proto.Value{
		0: proto.StrVal(c.Key),
		1: proto.StrVal(c.Name),
		2: proto.StrVal(c.Region),
		3: proto.U32(users),
		4: proto.U32(c.Kind),
		5: proto.U32(c.Opt5),
		6: f6,
		7: proto.U32(c.Number),
		8: proto.Bool(c.Opt8 != 0),
	})
}

// 聊天频道体系：频道模型、402 频道对象构造、在线人数统计，以及
// 24/5 chatlist / 24/6 create / 24/7 join 三个此前未实现的命令。
//
// 字段语义以 wiregen 的逆向标注为准（godhiicbgng / 402）：
//
//	f0 JSON 键 id          f1 JSON 键 name        f2 region（'GL'/'CN'）
//	f3 频道人数            f4 ChannelKind(0=Public;1=Private)
//	f5 典型值 0,1          f6 成员 uid 列表（大频道懒加载，可空）
//	f7 频道数字 id         f8 opt bool
//
// 详见 specs/changes/chat-channel-fix/spec.md。

// 聊天频道可见性（402 的 f4）。
const (
	chatChannelPublic  uint32 = 0
	chatChannelPrivate uint32 = 1
)

// 默认主频道：nav + 地图号 + 区域序号。
const (
	defaultChatChannelName   = "nav1_03"
	defaultChatChannelRegion = "CN"
	defaultChatChannelID     = uint32(1)
)

// chatChannelInfo 是服务端登记的频道（主频道与个人频道共用一张表）。
type chatChannelInfo struct {
	ID         string // 频道 id（裸名，如 "nav1_03"）
	Display    string // 频道显示名（402 的 f1）
	Region     string // 402 的 f2
	Kind       uint32 // 402 的 f4
	ChannelID  uint32 // 402 的 f7
	Members    map[int64]bool
	Persistent bool // 快照常驻频道：无人在线也保留（如羊村）
}

// chatChannelRegistry 是频道表。个人频道由 24/6 create 创建、24/7 join 加入；
// 主频道在首次使用时懒登记。表随 Server 生命周期存在，不落库。
type chatChannelRegistry struct {
	channels map[string]*chatChannelInfo
}

func newChatChannelRegistry() *chatChannelRegistry {
	r := &chatChannelRegistry{channels: map[string]*chatChannelInfo{}}
	// 把快照里的常驻私人频道预置为本服"羊村"：用官服真实 GUID 作 id、
	// 真实数字 id/region，Kind=Private。这样 24/1 move 与 24/7 join 按 GUID
	// 命中登记表后即为私人频道，不会被 ensureMainChannel 当成主频道，
	// 与主频道（nav 前缀）互不冲突。
	if _, featured := featuredChannels(); featured != nil {
		r.put(&chatChannelInfo{
			ID:         featured.Key,
			Display:    featured.Name,
			Region:     featured.Region,
			Kind:       chatChannelPrivate,
			ChannelID:  featured.Number,
			Members:    map[int64]bool{},
			Persistent: true,
		})
	}
	return r
}

// get 取频道；不存在返回 nil。
func (r *chatChannelRegistry) get(id string) *chatChannelInfo {
	if r == nil || id == "" {
		return nil
	}
	return r.channels[id]
}

// put 登记或覆盖频道。
func (r *chatChannelRegistry) put(info *chatChannelInfo) {
	if r == nil || info == nil || info.ID == "" {
		return
	}
	if r.channels == nil {
		r.channels = map[string]*chatChannelInfo{}
	}
	if info.Members == nil {
		info.Members = map[int64]bool{}
	}
	r.channels[info.ID] = info
}

// list 列出全部频道（按 id 排序，保证响应稳定）。
func (r *chatChannelRegistry) list() []*chatChannelInfo {
	if r == nil {
		return nil
	}
	out := make([]*chatChannelInfo, 0, len(r.channels))
	for _, c := range r.channels {
		out = append(out, c)
	}
	sortChatChannels(out)
	return out
}

// sortChatChannels 按频道 id 字典序排序（稳定输出）。
func sortChatChannels(list []*chatChannelInfo) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].ID < list[j-1].ID; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// ensureMainChannel 懒登记主频道。区域频道与主频道同构，只是 id 不同。
func (r *chatChannelRegistry) ensureMainChannel(id, region string) *chatChannelInfo {
	if c := r.get(id); c != nil {
		return c
	}
	c := &chatChannelInfo{
		ID:        id,
		Display:   mainChannelDisplay(id, region),
		Region:    region,
		Kind:      chatChannelPublic,
		ChannelID: defaultChatChannelID,
		Members:   map[int64]bool{},
	}
	r.put(c)
	return c
}

// mainChannelDisplay 主频道显示名。区域频道显示国名，缺省用公共频道。
func mainChannelDisplay(id, region string) string {
	switch region {
	case "CN":
		return "中国频道"
	case "RU":
		return "俄罗斯频道"
	case "DE":
		return "德国频道"
	case "EN":
		return "英国频道"
	case "FR":
		return "法国频道"
	case "PL":
		return "波兰频道"
	case "US":
		return "美国频道"
	case "JP":
		return "日本频道"
	case "KR":
		return "韩国频道"
	}
	return "公共频道"
}

// channelOnlineCount 统计指定频道的在线会话数（频道人数，402 的 f3）。
//
// 只数未关闭的会话；同 id 的多开会话各算一个（官服按会话计）。
// 不再给 0 人频道补 1：这个占位值会让客户端以为私人频道有人，
// 从而既不显示真实人数，也不会按原逻辑回收空频道。
func (s *Server) channelOnlineCount(channelID string) int64 {
	if s == nil {
		return 0
	}
	var n int64
	for _, sess := range s.channelPeers(channelID) {
		if sess != nil && !sess.Closed() {
			n++
		}
	}
	return n
}

// channelPeers 返回指定频道当前在线的会话快照（调用方不持有 s.mu）。
// 公聊广播与频道人数统计共用这一口径，避免“广播所有人、人数却按频道算”。
func (s *Server) channelPeers(channelID string) []*Session {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Session, 0, 8)
	for _, sess := range s.sessions {
		if sess == nil || sess.Closed() {
			continue
		}
		if sessChannel(sess).Name == channelID {
			out = append(out, sess)
		}
	}
	return out
}

// sweepEmptyPrivateChannels 回收无人在线的自建私人频道。
// 快照精选的常驻频道（Persistent）保留，否则"羊村"会在没人时被删掉，
// 之后就再也加入不了。会话断开、切频道后调用。
func (s *Server) sweepEmptyPrivateChannels() {
	if s == nil || s.chatChannels == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	occupied := make(map[string]bool, len(s.sessions))
	for _, sess := range s.sessions {
		if sess == nil || sess.Closed() {
			continue
		}
		occupied[sessChannel(sess).Name] = true
	}
	for id, info := range s.chatChannels.channels {
		if info == nil || info.Kind != chatChannelPrivate || info.Persistent {
			continue
		}
		if occupied[id] {
			continue
		}
		delete(s.chatChannels.channels, id)
		if s.log != nil {
			s.log.Info("私人频道无人在线，已自动删除", "channel", id)
		}
	}
}

// buildChannelObject 构造 402 频道对象（godhiicbgng）。这是唯一的构造点，
// 避免各处手写字段导致语义漂移。
func buildChannelObject(info *chatChannelInfo, users int64) *proto.Value {
	if info == nil {
		info = &chatChannelInfo{ID: defaultChatChannelName, Display: "公共频道",
			Region: defaultChatChannelRegion, Kind: chatChannelPublic, ChannelID: defaultChatChannelID}
	}
	display := info.Display
	if display == "" {
		display = mainChannelDisplay(info.ID, info.Region)
	}
	return wiregen.MustMakeObj("godhiicbgng", map[int]*proto.Value{
		0: proto.StrVal(info.ID),     // JSON 键 id
		1: proto.StrVal(display),     // JSON 键 name
		2: proto.StrVal(info.Region), // region
		3: proto.U32(uint32(users)),  // 频道人数
		4: proto.U32(info.Kind),      // ChannelKind
		5: proto.U32(0),              // 典型值 0,1
		6: proto.Null(),              // 成员列表懒加载
		7: proto.U32(info.ChannelID), // 频道数字 id
		8: proto.Bool(false),         // opt bool
	})
}

// chatChannelFromSession 把会话当前频道转成登记表条目（不写回登记表）。
func chatChannelFromSession(ch chatChannel) *chatChannelInfo {
	kind := chatChannelPublic
	if ch.Private {
		kind = chatChannelPrivate
	}
	return &chatChannelInfo{
		ID:        ch.Name,
		Display:   ch.Display,
		Region:    ch.Region,
		Kind:      kind,
		ChannelID: ch.ChannelID,
	}
}

// ---------------------------------------------------------------------------
// 24/5 chatlist / 24/6 create / 24/7 join
// ---------------------------------------------------------------------------

// hSocialChatList 处理 24/5 social/chatlist：按官服实包返回频道列表。
//
// 请求 args(524)[搜索关键字, 频道过滤]；响应 args(137)[obj402...]。
// 本服只列两类：主频道（快照 nav19_*，人数显示本服在线总数，恒置顶）与
// 私人频道（本服登记的 Private 频道，含快照里可加入的"梅利号空军基地"）。
// 其余 18000+ 官服快照条目不填充到本服列表。
func hSocialChatList(ctx *Ctx) ([]byte, bool) {
	strs := argStringsTop(ctx.Tree)
	keyword := ""
	if len(strs) > 0 {
		keyword = strings.ToLower(strings.TrimSpace(strs[0]))
	}

	mains, _ := featuredChannels()
	items := make([]*proto.Value, 0, 16)

	// 主频道人数用本服真实在线会话数，不照搬官服快照的在线数。
	var online uint32
	if ctx.Sess != nil && ctx.Sess.Srv != nil {
		online = uint32(ctx.Sess.Srv.SessionCount())
	}
	for _, c := range mains {
		items = append(items, buildOfficialChannelObject(c, online))
	}

	// 私人频道（含预置的"羊村"与本服 24/6 创建的频道）。
	if ctx.Sess != nil && ctx.Sess.Srv != nil {
		for _, info := range ctx.Sess.Srv.chatChannels.list() {
			if info == nil || info.Kind != chatChannelPrivate {
				continue
			}
			c := officialChatChannel{
				Key: info.ID, Name: info.Display, Region: info.Region,
				Kind: info.Kind, Number: info.ChannelID, Opt5: 1,
			}
			if keyword != "" && !chatChannelMatches(c, keyword) {
				continue
			}
			items = append(items, buildOfficialChannelObject(c, uint32(ctx.Sess.Srv.channelOnlineCount(info.ID))))
		}
	}

	code := "137"
	body, err := proto.EncodeValue(proto.Args(&code, items...), ctx.Sess.Table())
	if err != nil {
		ctx.Log.Warn("24/5 频道列表编码失败", "err", err)
		return emptyArgsBody(), true
	}
	return body, true
}

// chatChannelMatches 频道显示名或 key 包含关键字（小写比较）。
func chatChannelMatches(c officialChatChannel, keyword string) bool {
	return strings.Contains(strings.ToLower(c.Name), keyword) ||
		strings.Contains(strings.ToLower(c.Key), keyword)
}

// chatRequestArgs 宽松解析频道命令的请求参数：返回全部字符串与全部整数。
//
// 24/6 / 24/7 的报文字段顺序未抓样，不假设固定下标 —— 遍历取首个有效值，
// 避免因参数顺序假设错误而把频道认错。
func chatRequestArgs(ctx *Ctx) (strs []string, ints []int64) {
	strs = argStringsTop(ctx.Tree)
	if len(strs) == 0 {
		strs = deepStrings(ctx.Tree)
	}
	ints = deepInts(ctx.Tree)
	return strs, ints
}

// chatRequestChannelID 从请求里取频道 id：首个非空字符串。
func chatRequestChannelID(strs []string) string {
	for _, s := range strs {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

// chatRequestDisplay 从请求里取频道显示名：第二个非空字符串（首个是 id）。
func chatRequestDisplay(strs []string) string {
	seen := 0
	for _, s := range strs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		seen++
		if seen >= 2 {
			return s
		}
	}
	return ""
}

// hSocialCreateChannel 处理 24/6 social/create：创建个人频道。
//
// 个人频道是 Private（402 的 f4=1），登记进频道表后回该频道对象。
func hSocialCreateChannel(ctx *Ctx) ([]byte, bool) {
	strs, ints := chatRequestArgs(ctx)
	id := chatRequestChannelID(strs)
	if id == "" {
		id = "chat-" + itoa(int(ctx.Sess.Userid))
	}
	display := chatRequestDisplay(strs)
	if display == "" {
		display = id
	}
	region := defaultChatChannelRegion
	ch := sessChannel(ctx.Sess)
	if ch.Region != "" {
		region = ch.Region
	}
	info := &chatChannelInfo{
		ID:        id,
		Display:   display,
		Region:    region,
		Kind:      chatChannelPrivate,
		ChannelID: chatChannelNumberID(id, ints),
		Members:   map[int64]bool{ctx.Sess.Userid: true},
	}
	srv := ctx.Sess.Srv
	srv.mu.Lock()
	srv.chatChannels.put(info)
	srv.mu.Unlock()

	// 会话切到新频道。
	ctx.Sess.SetState("channel", map[string]any{
		"name":      info.ID,
		"display":   info.Display,
		"region":    info.Region,
		"users":     int64(1),
		"private":   true,
		"channelID": info.ChannelID,
	})
	srv.sweepEmptyPrivateChannels()
	ctx.Log.Info("24/6 创建个人频道", "channel", info.ID, "uid", ctx.Sess.Userid)
	body, err := proto.EncodeValue(buildChannelObject(info, 1), ctx.Sess.Table())
	if err != nil {
		return nil, false
	}
	return body, true
}

// hSocialJoinChannel 处理 24/7 social/join：加入频道。
//
// 已登记的个人频道 → Private；未登记的按主频道（Public）处理。
func hSocialJoinChannel(ctx *Ctx) ([]byte, bool) {
	strs, ints := chatRequestArgs(ctx)
	id := chatRequestChannelID(strs)
	if id == "" {
		id = defaultChatChannelName
	}
	srv := ctx.Sess.Srv
	srv.mu.Lock()
	info := srv.chatChannels.get(id)
	if info == nil {
		info = srv.chatChannels.ensureMainChannel(id, defaultChatChannelRegion)
		// 客户端请求里带了频道数字 id 时优先用它（402 的 f7）。
		info.ChannelID = chatChannelNumberID(id, ints)
	}
	if info.Members == nil {
		info.Members = map[int64]bool{}
	}
	info.Members[ctx.Sess.Userid] = true
	display := chatRequestDisplay(strs)
	if display != "" && info.Display == "" {
		info.Display = display
	}
	srv.mu.Unlock()

	users := srv.channelOnlineCount(info.ID)
	ctx.Sess.SetState("channel", map[string]any{
		"name":      info.ID,
		"display":   info.Display,
		"region":    info.Region,
		"users":     users,
		"private":   info.Kind == chatChannelPrivate,
		"channelID": info.ChannelID,
	})
	srv.sweepEmptyPrivateChannels()
	ctx.Log.Info("24/7 加入频道", "channel", info.ID, "kind", info.Kind, "uid", ctx.Sess.Userid)
	body, err := proto.EncodeValue(buildChannelObject(info, users), ctx.Sess.Table())
	if err != nil {
		return nil, false
	}
	return body, true
}

// chatChannelNumberID 频道数字 id（402 的 f7）：优先用请求里的整数，
// 否则由频道 id 派生一个稳定值。
func chatChannelNumberID(id string, ints []int64) uint32 {
	for _, v := range ints {
		if v > 0 && v <= 0xFFFFFFFF {
			return uint32(v)
		}
	}
	// 由 id 派生：同一 id 恒得同一数字 id。
	var h uint32 = 2166136261
	for i := 0; i < len(id); i++ {
		h ^= uint32(id[i])
		h *= 16777619
	}
	// 观测值域 >512，取 513 起。
	return 513 + h%100000
}
