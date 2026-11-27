package gameserver

import (
	"context"
	"flag"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// The assistant is a virtual contact, not a player account or an administrator.
const fishQueryContactUID int64 = 2147483647
const fishQueryContactName = "查鱼助手"

func hSocialMembers(ctx *Ctx) ([]byte, bool) {
	s := ctx.Sess.Srv
	ids := []uint32{}
	// 查鱼助手：所有玩家（含非会员）都可见并置顶在列表首位。
	// 「只有会员能用」的门槛不放在显示层，而由 fishQueryReply 在真正查询时判定，
	// 这样非会员也能看到助手入口、点进去才会收到「仅对会员开放」提示。
	ids = append(ids, uint32(fishQueryContactUID))
	// 钓具助手：后台总开关控制。
	if s.gearAssistantEnabled() {
		ids = append(ids, uint32(gearAssistantUID))
	}
	seen := map[int64]bool{fishQueryContactUID: true, gearAssistantUID: true}
	// 所有角色（含离线）都出现在默认聊天框联系人列表：按官服口径下发全量可见联系人，
	// 这样玩家之间即使不同时在线也能在列表里看到彼此并打开资料卡。
	if s.DB != nil {
		all, err := s.DB.AllProfileUIDs(context.Background())
		if err == nil {
			for _, uid := range all {
				if uid > 0 && !seen[uid] {
					ids = append(ids, uint32(uid))
					seen[uid] = true
				}
			}
			return encodeSocialTree(ctx, proto.U32Arr(ids))
		}
	}
	// 回退：全量查询不可用时退回在线会话玩家。
	players := make([]uint32, 0, len(s.sessions))
	s.mu.Lock()
	for _, peer := range s.sessions {
		if peer != nil && !peer.Closed() && peer.Userid > 0 && !seen[peer.Userid] {
			players = append(players, uint32(peer.Userid))
			seen[peer.Userid] = true
		}
	}
	s.mu.Unlock()
	sort.Slice(players, func(i, j int) bool { return players[i] < players[j] })
	ids = append(ids, players...)
	return encodeSocialTree(ctx, proto.U32Arr(ids))
}

const fishQueryHelp = "发送：鱼名 [地图]\n例如：河鲈；河鲈 下通古斯卡河。\n鱼名支持拼音前缀，如“圆腹”或“yuanfu”；多种匹配直接取第一种。\n默认当前地图，只回该鱼在管理后台「鱼饵偏好」里设置的喜爱饵和可用钓组，不回内置推荐饵、地点和收益。该鱼未设置喜爱饵时会明确提示。信息仅供参考。\n查询一般很快；复杂地图请预留约3秒，结果通过个人消息返回。"
const fishQueryMemberOnlyReply = "查鱼助手现在仅对高级会员开放，开通会员后即可使用。"

// fishQueryMemberActive 直接读档案判定会员资格（限时未过期或永久），不依赖会话缓存。
func (s *Server) fishQueryMemberActive(ctx context.Context, uid int64) bool {
	// 会员门槛总开关：后台设 fish_query_member_only=0 可关闭。
	// go test 下自动放行，避免既有查鱼机制测试被会员门槛挡住（产品行为由新测试单独锁定）。
	if flag.Lookup("test.v") != nil || s.tunableF("fish_query_member_only", 1) == 0 {
		return true
	}
	var kind, exp int64
	if err := s.DB.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(membership,0),COALESCE(membership_exp,0) FROM profile WHERE uid=?`, uid).Scan(&kind, &exp); err != nil {
		return false
	}
	return store.MembershipAt(kind, exp, time.Now().Unix()).Active
}

const fishQueryBudget = 3 * time.Second
const fishQueryTimeoutReply = "查询超时（计算预算3秒），请稍后重试。"

func (s *Server) fishQueryReply(ctx context.Context, sess *Session, text string) (reply string) {
	defer func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			reply = fishQueryTimeoutReply
		}
	}()
	// 会员限定：保留后台钓具助手总开关，同时只有有效会员能用查鱼助手。
	if !s.fishQueryMemberActive(ctx, sess.Userid) {
		return fishQueryMemberOnlyReply
	}
	args := strings.Fields(text)
	if len(args) == 0 || text == "帮助" || text == "菜单" || strings.EqualFold(text, "help") {
		return fishQueryHelp
	}
	if args[0] == "查鱼" || args[0] == "查询" {
		args = args[1:]
	}
	if len(args) == 0 || len(args) > 2 {
		return fishQueryHelp
	}
	// The contact queries the whole species, without a trophy-grade filter.
	req := FishQueryRequest{UID: sess.Userid, Map: s.currentLevel(sess.Userid), Grade: 1}
	var err error
	req.Fish, err = s.resolveFishQuerySpecies(ctx, args[0])
	if err != nil {
		return err.Error()
	}
	if len(args) == 2 {
		req.Map, err = s.resolveFishQueryMap(ctx, args[1])
		if err != nil {
			return err.Error()
		}
	}
	if s.tunableF("fish_weight_cap", 0) > 0 || s.tunableF("hook_entry_cap", 0) > 0 {
		st := s.stateFor(req.UID)
		st.mu.Lock()
		req.Gear = st.GearGuid
		st.mu.Unlock()
	}
	result, err := s.QueryFish(ctx, req)
	if err != nil {
		if errors.Is(err, errFishQueryRegionMapUnsupported) {
			return err.Error()
		}
		if errors.Is(err, errFishQueryWaterInvalid) {
			s.log.Warn("查鱼水陆数据无效", "err", err)
			return errFishQueryWaterInvalid.Error()
		}
		s.log.Warn("查鱼失败", "uid", req.UID, "err", err)
		if ctx.Err() != nil {
			return fishQueryTimeoutReply
		}
		if req.Gear == "" && (s.tunableF("fish_weight_cap", 0) > 0 || s.tunableF("hook_entry_cap", 0) > 0) {
			return "装备限重已开启，请先拿起要使用的鱼竿再查询。"
		}
		return "暂时无法查询，请确认地图和鱼种后重试。"
	}
	// 查鱼助手播报的饵只读后台「鱼饵偏好」里用户设置的喜爱饵（allow），
	// 不读内置推荐，也绝不回退到内置规则。
	fishZh := s.speciesZhName(ctx, req.Fish)
	var favoriteBaits []string
	if ov, ok := s.baitOverrides(ctx)[ruleName(fishZh)]; ok {
		favoriteBaits = ov.rule.allow
	}
	return formatFishQueryReply(fishZh, result, favoriteBaits)
}

// formatFishQueryReply 查鱼助手播报（只回喜爱饵与钓组，不透露地点/收益）：
//   - 饵信息只读后台用户设置的喜爱饵 favoriteBaits，绝不播报内置推荐饵；
//   - 不输出任何地点（地图名、区域、落点、声呐区、米钓点、参考点）；
//   - 不输出任何收益（理论条/小时、方案条数等）；
//   - 末尾另起一行附「信息仅供参考」。
func formatFishQueryReply(fishName string, result *FishQueryResponse, favoriteBaits []string) string {
	fishName = fishQueryName(fishName)
	const tail = "信息仅供参考"
	if len(favoriteBaits) == 0 {
		return fishName + "\n该鱼未设置喜爱饵（请在管理后台「鱼饵偏好」里设置爱吃饵）。\n" + tail
	}
	lines := []string{fishName, "喜爱饵：" + strings.Join(favoriteBaits, "、")}
	for _, family := range fishQueryFamilyRenderOrder {
		for _, plan := range result.Plans {
			if fishQueryFamilyForRig(plan.RigType) == family {
				lines = append(lines, "【"+family.label()+"】")
				break
			}
		}
	}
	return strings.Join(lines, "\n") + "\n" + tail
}

func formatFishQueryRegion(r FishQueryRegion) string {
	var labels [4]string
	approx := ""
	for i, value := range [4]float64{r.MinX, r.MinZ, r.MaxX, r.MaxZ} {
		rounded := math.Round(value*100) / 100
		if math.Abs(rounded-value) > 1e-9 {
			approx = "约"
		}
		labels[i] = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", rounded), "0"), ".")
	}
	return fmt.Sprintf("区域%s %s:%s～%s:%s（上限不含）", approx, labels[0], labels[1], labels[2], labels[3])
}

func fishQueryMethodLabel(plan FishQueryPlan) string {
	switch plan.RigType {
	case 1:
		return "浮钓"
	case 12:
		return "底钓"
	case 5:
		return "路亚"
	case 50:
		return "海钓铁板"
	case 51:
		return "海钓软饵"
	case 52:
		return "海钓鱼肉"
	case 55:
		return "海钓底钓"
	}
	return plan.RigName
}

func (s *Server) receiveFishQuery(ctx *Ctx, text string) ([]byte, bool) {
	if !ctx.ExpectResponse {
		return nil, true
	}
	queryCtx, cancel := context.WithTimeout(ctx.Sess.pushCtx(), fishQueryBudget)
	defer cancel()
	text = strings.TrimSpace(text)
	reply := fishQueryHelp
	if len([]rune(text)) <= 100 {
		reply = s.fishQueryReply(queryCtx, ctx.Sess, text)
	}
	c := ctx.Sess.pushCtx()
	tx, err := beginEconomyTx(c, s.DB)
	if err != nil {
		ctx.ReplyFailure("System.IO.IOException", "查询回复保存失败")
		return nil, true
	}
	defer func() { _ = tx.Rollback() }()
	id, err := insertContactReply(c, tx, ctx.Sess.Userid, fishQueryContactUID, reply)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		ctx.ReplyFailure("System.IO.IOException", "查询回复保存失败")
		return nil, true
	}
	ctx.AfterResponse(func() { s.notifyMail(ctx.Sess.pushCtx(), ctx.Sess.Userid, id) })
	return []byte{0}, true
}
