package gameserver

import (
	"sort"
	"time"
	"database/sql"
	"encoding/json"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
)

func registerSocialHandlers(d *Dispatch) {
	d.Register(24, 4, hSocialMembers)
	d.Register(24, 5, hSocialChatList)
	d.Register(24, 6, hSocialCreateChannel)
	d.Register(24, 7, hSocialJoinChannel)
	d.RegisterResponseContract(24, 4, ResponseContractRule{Kind: proto.KindU32Arr})
	d.Register(24, 18, hSocialUserProfile)
	d.Register(24, 19, hSocialUserCard)
	d.Register(24, 21, hSocialAbsoluteRecords)
	d.Register(24, 22, hSocialRecordFish)
	d.Register(24, 24, hSocialNamedFish)
}

func firstRequestUID(ctx *Ctx) int64 {
	if v, ok := ctx.ArgInt(0); ok {
		return int64(v)
	}
	if ints := deepInts(ctx.Tree); len(ints) > 0 {
		return ints[0]
	}
	return 0
}

func encodeSocialTree(ctx *Ctx, v *proto.Value) ([]byte, bool) {
	body, err := proto.EncodeValue(v, ctx.Sess.Table())
	if err != nil {
		ctx.Log.Error("24/x 响应编码失败", "cmd", ctx.Key(), "err", err)
		return nil, false
	}
	return body, true
}

func bytesWrappedObject(inner *proto.Value, table *proto.OpsTable) (*proto.Value, error) {
	raw, err := proto.EncodeValue(inner, table)
	if err != nil {
		return nil, err
	}
	return proto.BytesOf(raw), nil
}

// profileQualification 是玩家一种资格的实际达成情况，f4 资格徽章与 f7 资格
// 条目共用同一份计算结果，保证两处口径一致。
type profileQualification struct {
	branch     int64
	stage      int64 // 实际达到的最高阶段，1..qualificationStages
	badgeID    uint32
	completedTS float64
}

// collectProfileQualifications 从资格状态汇总每种永久分支（1..qualificationBranches）
// 的实际等级与完成时间。RuleID = branch*1000+(stage-1)*100+task；未完成不算。
//
// ★ 等级口径：一个阶段必须 3 条 task 全部完成才算「通过该阶段」，等级取从
// stage1 起连续通过的最大值（用户明确选择的 C 口径）。断档即停在断档处：
// 例如只完成 stage1/2/3/7 各一条时等级=0（stage1 未集齐 3 条）。
// 完成时间取已通过阶段中最后一条（最大时间戳）。
//
// ★ 全部永久分支都保留，包括没有官服徽章样本的分支 7（鲤鱼捕钓）与 8（海洋钓鱼）。
// f7 用「分支 ID」下发给客户端查 qlfctn_n_<分支> 名称/图标，分支 7/8 完全合法；
// badgeID=0 的分支只影响 f4（徽章墙）——f4 构造时会跳过，不往 typed_u32arr 塞 0。
func collectProfileQualifications(states map[int64]store.QualificationState) []profileQualification {
	type taskKey struct{ stage, task int64 }
	done := make(map[int64]map[taskKey]float64) // branch -> (stage,task) -> 完成时间
	for ruleID, st := range states {
		if st.CompletedTS <= 0 || ruleID < 1000 || ruleID > qualificationBranches*1000+999 {
			continue
		}
		branch, rem := ruleID/1000, ruleID%1000
		if branch < 1 || branch > qualificationBranches {
			continue
		}
		stage, task := rem/100+1, rem%100
		if stage < 1 || stage > qualificationStages || task < 1 || task > qualificationTasks {
			continue
		}
		if done[branch] == nil {
			done[branch] = make(map[taskKey]float64)
		}
		k := taskKey{stage, task}
		if st.CompletedTS > done[branch][k] {
			done[branch][k] = st.CompletedTS
		}
	}
	out := make([]profileQualification, 0, len(done))
	for branch, tasks := range done {
		q := profileQualification{branch: branch}
		for stage := int64(1); stage <= qualificationStages; stage++ {
			complete := true
			var stageTS float64
			for task := int64(1); task <= qualificationTasks; task++ {
				ts, ok := tasks[taskKey{stage, task}]
				if !ok {
					complete = false
					break
				}
				if ts > stageTS {
					stageTS = ts
				}
			}
			if !complete {
				break
			}
			q.stage = stage
			q.completedTS = stageTS
		}
		if q.stage == 0 {
			continue // 第一阶段都没集齐，不下发该分支
		}
		q.badgeID = qualificationBadgeID(q.branch, q.stage) // 7/8 为 0，f4 会跳过
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].branch < out[j].branch })
	return out
}

// buildProfileAchievementBadges 构造 24/18.f4 的 typed_u32arr(311) 成就徽章墙。
//
// ★ 数据源是 achievement_def + player_achievements（与「统计」3/3 完全同一套
// ID 和解锁状态），不是资格任务进度。资格分支等级走 f7，不要再往 f4 塞
// qualificationBadgeID —— 那会让已解锁的十几/几十条成就只剩 4 条。
//
// 顺序按成就定义的 sort,id（AchievementDefinitions 已排好），与客户端统计面板
// 一致；定义表里查不到的已解锁 ID 按数值升序补在末尾，避免漏发。
func buildProfileAchievementBadges(defs []int64, states map[int64]store.AchievementState) *proto.Value {
	ids := make([]uint32, 0, len(states))
	seen := make(map[int64]bool, len(defs))
	for _, id := range defs {
		st, ok := states[id]
		if !ok || !st.Unlocked || seen[id] {
			continue
		}
		ids = append(ids, uint32(id))
		seen[id] = true
	}
	extras := make([]int64, 0)
	for id, st := range states {
		if st.Unlocked && !seen[id] {
			extras = append(extras, id)
		}
	}
	sort.Slice(extras, func(i, j int) bool { return extras[i] < extras[j] })
	for _, id := range extras {
		ids = append(ids, uint32(id))
	}
	return proto.TypedU32Arr("311", ids)
}

// buildProfileQualificationEntries 构造 24/18.f7 的 args(207) obj5010 列表。
//
// ★ 按用户明确要求偏离官服语义：f7 不填水域排名，而是逐条对应玩家实际拥有的
// 资格分支——f0=分支ID(1..20)、f1=该分支连续通过等级(1..7，每阶段3条全做完才算
// 通过)、f2=完成时间 FILETIME。这样资格页显示的是真实资格等级，而不是之前按
// grade>=3 渔获数递增出来的"等级1~1769"。
//
// ★★ f0 必须是「分支 ID」而不是徽章 ID：客户端资格页按 qlfctn_n_<分支ID> 查
// 名称与勋章图标（见 timeline.go qualificationBranchNode 的 Idx0 注释）。
// 徽章 ID（153/253/307…）远超分支范围，客户端查不到就 fallback，名字与图标都
// 显示不出来；此前把 f0 填成徽章 ID 正是"等级对了但名字图标坏了"的根因。
// 第一版填水域小整数能显示，只是因为 1..20 恰好被误当成分支 ID。
func buildProfileQualificationEntries(quals []profileQualification) *proto.Value {
	items := make([]*proto.Value, 0, len(quals))
	for _, q := range quals {
		items = append(items, wiregen.MustMakeObj("iahhndknkbe", map[int]*proto.Value{
			0: proto.U32(uint32(q.branch)), // 分支ID → qlfctn_n_<branch> 名称/勋章
			1: proto.U32(uint32(q.stage)),  // 该分支实际最高等级 1..7
			2: proto.U64(unixToFiletime100ns(q.completedTS)),
		}))
	}
	return proto.Args(proto.S("207"), items...)
}

func hSocialUserProfile(ctx *Ctx) ([]byte, bool) {
	uid := firstRequestUID(ctx)
	decks := tradingDeckList()
	if s := ctx.Sess.Srv; s != nil && s.DB != nil {
		var err error
		decks, err = s.playerTradingDecks(ctx.Sess.pushCtx(), uid)
		if err != nil {
			ctx.ReplyFailure("System.IO.IOException", "卡册记录读取失败")
			return nil, true
		}
	}
	// 用户补丁（官服抓包 2026-11-25 校准）：24/18 顶层 10 字段，此前只填 f0/f9，
	// 客户端打开他人资料卡页会因 f1/f2 缺失而报错空白。
	// f1=obj322 统计快照、f2=obj321 鱼类记录（复用 3/2 的现成构造），
	// f3=空 args(186)、f8=空串；f4~f7/f9 保留既有/空形状。
	fields := map[int]*proto.Value{
		0: proto.U32(uint32(uid)),
		9: decks,
	}
	if s := ctx.Sess.Srv; s != nil && s.DB != nil {
		if stats322 := buildStats322W(worldTotalGameMin(time.Now())); stats322 != nil {
			fields[1] = stats322
		}
		// 官服 f2=obj321 鱼类记录：复用 3/2 的 buildStats321，鱼卡片/纪录卡完整。
		// 经服务端解码器验证嵌套编码合法（netdecode 工具的 0x30 报错是工具限制，非真实问题）。
		if stats321, err := s.buildStats321(ctx.Sess.pushCtx(), uid); err == nil && stats321 != nil {
			fields[2] = stats321
		}
	}
	// 官服抓包 f3~f8：页签数据。f4=成就ID、f5=奖赏、f6=稀有鱼/水域纪录、f7=稀有鱼排名。
	fields[3] = proto.Args(proto.S("186"))
	fields[8] = proto.StrVal("")
	if s := ctx.Sess.Srv; s != nil && s.DB != nil {
		cctx := ctx.Sess.pushCtx()
		// ★ f4 成就徽章墙：读「统计」3/3 同一套成就（achievement_def /
		// player_achievements），不是资格任务进度。此前把资格徽章塞进 f4，
		// 导致已解锁成就只显示零星几条（且名称/图标错乱）。
		defs, defsErr := s.DB.AchievementDefinitions(cctx)
		achStates, achErr := s.DB.PlayerAchievementStates(cctx, uid)
		if defsErr == nil && achErr == nil {
			fields[4] = buildProfileAchievementBadges(defs, achStates)
		}
		// f7 资格条目：f0=分支ID(查名称/图标)、f1=实际等级、f2=完成时间。
		var quals []profileQualification
		if states, err := s.DB.QualificationStates(cctx, uid); err == nil {
			quals = collectProfileQualifications(states)
		}
		// f5 奖赏：复用 3/16 的 obj2015 列表。
		if rows, err := s.playerRewardItems(cctx, uid); err == nil {
			items := make([]*proto.Value, 0, len(rows))
			for _, it := range rows {
				if obj, e := makeRewardEntry(it); e == nil { items = append(items, obj) }
			}
			fields[5] = proto.Args(proto.S("185"), items...)
		}
		// f6 稀有鱼：复用 3/17 的 args(132) obj323 水域分组。
		if trophies, err := s.buildFishTrophies323(cctx, uid); err == nil && trophies != nil {
			fields[6] = trophies
		}
		fields[7] = buildProfileQualificationEntries(quals)
	}
	if _, ok := fields[4]; !ok { fields[4] = proto.TypedU32Arr("311", nil) }
	if _, ok := fields[5]; !ok { fields[5] = proto.Args(proto.S("185")) }
	if _, ok := fields[6]; !ok { fields[6] = proto.Args(proto.S("132")) }
	if _, ok := fields[7]; !ok { fields[7] = proto.Args(proto.S("207")) }
	inner := wiregen.MustMakeObj("feaidppnpch", fields)
	outer, err := bytesWrappedObject(inner, ctx.Sess.Table())
	if err != nil {
		ctx.Log.Error("24/18 档案编码失败", "uid", uid, "err", err)
		return []byte{0x00}, true
	}
	return encodeSocialTree(ctx, outer)
}

func hSocialUserCard(ctx *Ctx) ([]byte, bool) {
	uid := firstRequestUID(ctx)
	if isVirtualContact(uid) {
		name := fishQueryContactName
		if uid == gearAssistantUID {
			name = gearAssistantName
		}
		outer, err := bytesWrappedObject(virtualContactCard(uid, name), ctx.Sess.Table())
		if err != nil {
			return nil, false
		}
		return encodeSocialTree(ctx, outer)
	}
	var p *store.Profile
	names := ""
	avatar := "" // 头像 URL（f2）：默认空串，从 profile.avatar 读取后台设置的完整 URL
	level := int64(1)
	role := int64(0)
	region := int64(0)
	trophies := int64(0)
	if ctx.Sess.Srv != nil && ctx.Sess.Srv.DB != nil {
		var err error
		p, err = ctx.Sess.Srv.DB.GetProfile(ctx.Sess.Srv.dbCtx(), uid)
		if err != nil && err != sql.ErrNoRows {
			ctx.Log.Error("24/19 档案查询失败", "uid", uid, "err", err)
		}
		if p != nil {
			names = p.Name
			avatar = p.Avatar // 下发后台设置的头像，修复其他玩家显示默认头像的问题
			level = p.Level
			role = p.Role
			region = socialRegionCode(p.Region)
			// f5 与 profile f16 同落客户端 ifbfdldbana.gebeadddmlp（等级旁
			// 星号同一字段），官服按鱼种去重计，不按稀有渔获条数。
			if n, err := ctx.Sess.Srv.DB.RareSpeciesCount(ctx.Sess.Srv.dbCtx(), uid, 3); err == nil {
				trophies = n
			}
		}
	}
	// f6 是「在线（online）」字段：客户端据此点亮/熄灭联系人列表里的在线灯。
	// 此前恒传 0，在线灯永远灭着。
	online := int64(0)
	if ctx.Sess.Srv != nil && ctx.Sess.Srv.userOnline(uid) {
		online = 1
	}
	inner := wiregen.MustMakeObj("dgbmlekfpbg", map[int]*proto.Value{
		0: proto.U32(uint32(uid)),
		1: proto.StrVal(names),
		2: proto.Str(&avatar, 0x15),
		3: proto.U32(uint32(role)),
		4: proto.U32(uint32(level)),
		5: proto.U32(uint32(trophies)),
		6: proto.U32(uint32(online)),
		7: proto.U32(uint32(region)),
		8: proto.U32(0),
	})
	outer, err := bytesWrappedObject(inner, ctx.Sess.Table())
	if err != nil {
		ctx.Log.Error("24/19 资料卡编码失败", "uid", uid, "err", err)
		return []byte{0x00}, true
	}
	return encodeSocialTree(ctx, outer)
}

func socialRegionCode(region string) int64 {
	switch region {
	case "GL":
		return 0
	case "RU":
		return 1
	case "DE":
		return 2
	case "EN":
		return 3
	case "CN":
		return 4
	case "FR":
		return 5
	case "PL":
		return 6
	case "US":
		return 7
	case "JP":
		return 8
	case "KR":
		return 9
	default:
		return 0
	}
}

func hSocialAbsoluteRecords(ctx *Ctx) ([]byte, bool) {
	strs := argStringsTop(ctx.Tree)
	pick := func(i int, def string) string {
		if i < len(strs) && strs[i] != "" {
			return strs[i]
		}
		return def
	}
	payload := struct {
		Success  bool              `json:"success"`
		ID       string            `json:"id"`
		Type     string            `json:"type"`
		Locale   string            `json:"locale"`
		Region   string            `json:"region"`
		Modified int64             `json:"modified"`
		Records  []json.RawMessage `json:"records"`
	}{
		Success: true, ID: pick(0, "absolute"), Type: pick(1, "DEF"),
		Locale: pick(2, "zh-CN"), Region: "GL", Records: []json.RawMessage{},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		ctx.Log.Error("24/21 JSON 编码失败", "err", err)
		return []byte{0x00}, true
	}
	return encodeSocialTree(ctx, proto.BytesOf(raw))
}

func hSocialRecordFish(ctx *Ctx) ([]byte, bool) {
	water := argFirstStr(ctx.Tree)
	if ctx.Sess.Srv == nil || ctx.Sess.Srv.DB == nil {
		return encodeSocialTree(ctx, proto.Null())
	}
	row, err := ctx.Sess.Srv.DB.BestCatchByWater(ctx.Sess.Srv.dbCtx(), ctx.Sess.Userid, water)
	if err != nil {
		ctx.Log.Error("24/22 纪录鱼查询失败", "water", water, "err", err)
		return encodeSocialTree(ctx, proto.Null())
	}
	if row == nil {
		return encodeSocialTree(ctx, proto.Null())
	}
	angler := ""
	if p, err := ctx.Sess.Srv.DB.GetProfile(ctx.Sess.Srv.dbCtx(), ctx.Sess.Userid); err == nil {
		angler = p.Name
	}
	loc := row.LocLevel
	if loc == "" {
		loc = row.Loc
	}
	// f11（oajeeklppia）同样是模型族参数：留空就退回鱼种默认族（老模型），
	// 纪录页的预览会跟渔护/溜鱼对不上。这里用 catch_guid 推导同一个族名。
	modelKey := ctx.Sess.Srv.fishModelVariantForFish(row.FishKey, row.WeightG, row.LengthM,
		row.Grade, row.GUID)
	v := wiregen.MustMakeObj("kijhfkcpngc", map[int]*proto.Value{
		0:  proto.StrVal(row.FishKey),
		1:  proto.U32(uint32(row.WeightG)),
		2:  proto.F32(float32(row.LengthM)),
		3:  proto.U32(uint32(maxInt64(row.Grade, 1))),
		4:  proto.U32(uint32(row.XP)),
		5:  proto.U64(unixToFiletime100ns(row.TS)),
		6:  proto.U32(0),
		7:  proto.StrVal(angler),
		8:  proto.StrVal(""),
		9:  proto.StrVal(loc),
		10: proto.ArrayOf(nil),
		11: proto.StrVal(modelKey),
	})
	return encodeSocialTree(ctx, v)
}

func hSocialNamedFish(ctx *Ctx) ([]byte, bool) {
	key := argFirstStr(ctx.Tree)
	payload := struct {
		Success   bool              `json:"success"`
		Modified  int64             `json:"modified"`
		PlaceID   string            `json:"placeid"`
		NamedFish []json.RawMessage `json:"namedfish"`
	}{Success: true, PlaceID: key, NamedFish: []json.RawMessage{}}
	raw, err := json.Marshal(payload)
	if err != nil {
		ctx.Log.Error("24/24 JSON 编码失败", "err", err)
		return []byte{0x00}, true
	}
	return encodeSocialTree(ctx, proto.BytesOf(raw))
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
