package gameserver

// 在线状态判定：客户端的「在线灯」依赖服务端给出的真实在线状态。
//
// 背景：24/19 资料卡（2002 dgbmlekfpbg）的 f6 字段语义是「在线（online）」，
// 客户端据此点亮/熄灭联系人列表里的在线灯。此前该字段恒传 0，在线灯永远灭着 ——
// 无论对方是否真的在线。
//
// 独立成文件的原因：在线灯与聊天频道体系是两件事。
//
//	聊天频道（chat_channels.go）：频道模型、402 频道对象、频道人数、24/5~24/7。
//	在线灯（本文件）：只判定"某个 uid 当前是否有活跃会话"。
//
// 两者唯一的交集是本文件里的 userOnline —— 它被 24/19 的资料卡构造调用。
// 分开成文件后，在线灯可以独立成补丁应用，不必连带整个聊天频道体系。

// userOnline 目标 uid 是否有未关闭的在线会话（24/19 资料卡的在线灯，f6）。
//
// 判定依据是服务端会话表：sessions 里存在一个未关闭、且 Userid 匹配的会话，
// 即视为在线。断线重连期间旧会话尚未清理、新会话已建立，两者都算在线 ——
// 对客户端而言，玩家确实能收到消息。
func (s *Server) userOnline(uid int64) bool {
	if s == nil || uid <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if sess != nil && !sess.Closed() && sess.Userid == uid {
			return true
		}
	}
	return false
}
