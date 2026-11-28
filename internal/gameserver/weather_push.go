package gameserver

import (
	"time"

	"rf4server/internal/proto"
)

// pushWeatherRefresh 给所有在线玩家重发其当前地图的 1/12 dob 响应，
// 让客户端立刻看到新的天气/温度/风速。只推仍在正常地图的会话；
// 没有当前地图的会话跳过。每个会话单独构造 dob（地图 key 不同）。
func (s *Server) pushWeatherRefresh() (sent int) {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	peers := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		if sess == nil || sess.Closed() {
			continue
		}
		peers = append(peers, sess)
	}
	s.mu.Unlock()

	now := time.Now().Unix()
	for _, sess := range peers {
		key := s.currentLevel(sess.Userid)
		if key == "" {
			continue
		}
		dobVal, err := s.buildMapDobValue(sess.Userid, key, now)
		if err != nil {
			continue
		}
		body, err := proto.EncodeValue(dobVal, s.Table())
		if err != nil {
			continue
		}
		// 作为 1/12 响应重发（callID=0），客户端按地图 dob 刷新天气显示；
		// 同时会话天气计划随 rememberWeather 重锚，判鱼与显示一致。
		sess.SendEnvelope(proto.BuildResponseEnvelope(0, body))
		sess.rememberWeather(key, now)
		sent++
	}
	return sent
}
