package gameserver

import (
	"encoding/json"
	"strings"
	"time"

	"rf4server/internal/proto"
)

// 游戏内记录榜（24/21 / 24/24）按官服 2026-11-25 实包形状组装。
// 官服证据：响应是 bytes 包装的 JSON，24/21 顶层 7 键，每鱼种 Top5，
// 每条记录 7 字段（skin/weight/location/bait_lure/date/exp/user）；
// 24/24 顶层 4 键（success/modified/placeid/namedfish），history 5 字段。
// 数据来自本服 catch_log（全服玩家真实钓获），不再返回空数组。

type officialLeaderboardLocation struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type officialLeaderboardBait struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type officialLeaderboardUser struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
	Level  int64  `json:"level"`
	Online bool   `json:"online"`
	Pvp    int64  `json:"pvp"`
	Trph   int64  `json:"trph"`
	Role   int64  `json:"role"`
	Qual   int64  `json:"qual"`
	Region string `json:"region"`
}

type officialLeaderboardRecord struct {
	Skin     *string                   `json:"skin"`
	Weight   int64                     `json:"weight"`
	Location officialLeaderboardLocation `json:"location"`
	BaitLure officialLeaderboardBait   `json:"bait_lure"`
	Date     int64                     `json:"date"`
	Exp      int64                     `json:"exp"`
	User     officialLeaderboardUser   `json:"user"`
}

type officialLeaderboardFish struct {
	Fish struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"fish"`
	Records []officialLeaderboardRecord `json:"records"`
}

func regionNameByCode(code int64) string {
	switch code {
	case 0:
		return "GL"
	case 2:
		return "DE"
	case 4:
		return "CN"
	default:
		return "GL"
	}
}

// leaderboardRegionCode 从请求里取 region code。官服请求为
// args(524)[str id, u32 region, str "DEF", str locale]；忽略 524 本身。
func leaderboardRegionCode(ctx *Ctx) int64 {
	for _, n := range deepInts(ctx.Tree) {
		if n == 524 {
			continue
		}
		if n == 0 || n == 2 || n == 4 {
			return n
		}
	}
	return 0
}

// leaderboardBaitName 把饵 key（可能是 "a;b" 组合）映射为中文名。
func leaderboardBaitName(id string, names map[string]string) string {
	if id == "" {
		return ""
	}
	parts := strings.Split(id, ";")
	labels := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if n, ok := names[p]; ok && n != "" {
			labels = append(labels, n)
		} else {
			labels = append(labels, p)
		}
	}
	return strings.Join(labels, "; ")
}

// serverOnlineUIDs 当前在线玩家 uid 集合（官服 user.online）。
func (s *Server) serverOnlineUIDs() map[int64]bool {
	out := map[int64]bool{}
	if s == nil {
		return out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if sess != nil && !sess.Closed() {
			out[sess.Userid] = true
		}
	}
	return out
}

// buildAbsoluteRecordsPayload 生成 24/21 官服形状 JSON。
func (s *Server) buildAbsoluteRecordsPayload(id, typ, locale string, regionCode int64) []byte {
	payload := struct {
		Success  bool                       `json:"success"`
		ID       string                     `json:"id"`
		Type     string                     `json:"type"`
		Locale   string                     `json:"locale"`
		Region   string                     `json:"region"`
		Modified int64                      `json:"modified"`
		Records  []officialLeaderboardFish  `json:"records"`
	}{
		Success: true, ID: id, Type: typ, Locale: locale,
		Region: regionNameByCode(regionCode), Modified: time.Now().Unix(),
		Records: []officialLeaderboardFish{},
	}
	if s == nil || s.DB == nil {
		raw, _ := json.Marshal(payload)
		return raw
	}
	ctx := s.dbCtx()
	rows, err := s.DB.GlobalLeaderboard(ctx, id, 5)
	if err != nil {
		raw, _ := json.Marshal(payload)
		return raw
	}
	profiles, _ := s.DB.LeaderboardProfiles(ctx)
	locs, _ := s.DB.LeaderboardLocNames(ctx)
	baits, _ := s.DB.LeaderboardBaitNames(ctx)
	online := s.serverOnlineUIDs()

	for _, f := range rows {
		entry := officialLeaderboardFish{}
		entry.Fish.ID, entry.Fish.Name = f.FishKey, f.FishName
		if entry.Fish.Name == "" {
			entry.Fish.Name = f.FishKey
		}
		entry.Records = make([]officialLeaderboardRecord, 0, len(f.Records))
		for _, r := range f.Records {
			locID := r.LocLevel
			if locID == "" {
				locID = r.Loc
			}
			locName := locs[locID]
			if locName == "" {
				locName = locID
			}
			p := profiles[r.UID]
			user := officialLeaderboardUser{
				ID: r.UID, Name: p.Name, Avatar: p.Avatar, Level: p.Level,
				Online: online[r.UID], Role: p.Role, Region: p.Region,
			}
			if user.Name == "" {
				user.Name = "player"
			}
			if user.Region == "" {
				user.Region = "CN"
			}
			entry.Records = append(entry.Records, officialLeaderboardRecord{
				Weight: r.WeightG,
				Location: officialLeaderboardLocation{ID: locID, Name: locName},
				BaitLure: officialLeaderboardBait{ID: r.Bait, Name: leaderboardBaitName(r.Bait, baits)},
				Date: int64(r.TS), Exp: r.XP, User: user,
			})
		}
		payload.Records = append(payload.Records, entry)
	}
	raw, _ := json.Marshal(payload)
	return raw
}

type officialNamedFishHistory struct {
	ID      int64   `json:"id"`
	Weight  float64 `json:"weight"`
	Length  float64 `json:"length"`
	BaitID  string  `json:"baitID"`
	Catched int64   `json:"catched"`
}

type officialNamedFishEntry struct {
	ID       string                     `json:"id"`
	Name     string                     `json:"name"`
	SystemID string                     `json:"systemID"`
	History  []officialNamedFishHistory `json:"history"`
}

// buildNamedFishPayload 生成 24/24 官服形状 JSON（顶层 4 键）。
func (s *Server) buildNamedFishPayload(place string) []byte {
	payload := struct {
		Success   bool                      `json:"success"`
		Modified  int64                     `json:"modified"`
		PlaceID   string                    `json:"placeid"`
		NamedFish []officialNamedFishEntry  `json:"namedfish"`
	}{
		Success: true, Modified: time.Now().Unix(), PlaceID: place,
		NamedFish: []officialNamedFishEntry{},
	}
	if s == nil || s.DB == nil {
		raw, _ := json.Marshal(payload)
		return raw
	}
	rows, err := s.DB.GlobalNamedFish(s.dbCtx(), place)
	if err != nil {
		raw, _ := json.Marshal(payload)
		return raw
	}
	for _, nf := range rows {
		e := officialNamedFishEntry{ID: nf.FishID, Name: nf.Name, SystemID: nf.Species,
			History: []officialNamedFishHistory{}}
		for _, h := range nf.History {
			e.History = append(e.History, officialNamedFishHistory{
				ID: h.ID, Weight: float64(h.WeightG) / 1000, Length: h.LengthM,
				BaitID: h.Bait, Catched: int64(h.TS),
			})
		}
		payload.NamedFish = append(payload.NamedFish, e)
	}
	raw, _ := json.Marshal(payload)
	return raw
}

// encodeLeaderboardJSON 把 JSON 按 bytes 包装成 24/x 响应。
func encodeLeaderboardJSON(ctx *Ctx, raw []byte) ([]byte, bool) {
	return encodeSocialTree(ctx, proto.BytesOf(raw))
}
