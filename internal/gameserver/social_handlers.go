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
		// f4 资格徽章：官服 typed_u32arr(311) 每种资格只发最高等级的一个ID，
		// 未获得的资格不下发（官服 hides，不显示灰色剪影）。此前误填成就ID
		// → 客户端渲染成12个同款「传统纺车竿」+大量无效数据。
		// RuleID = branch*1000+(stage-1)*100+task；短ID = branch*100+stage。
		best := map[int64]int64{} // branch -> 最高已完成 stage
		if states, err := s.DB.QualificationStates(cctx, uid); err == nil {
			for ruleID, st := range states {
				if st.CompletedTS <= 0 || ruleID < 1000 || ruleID > 8999 { continue }
				branch, rem := ruleID/1000, ruleID%1000
				stage := rem/100 + 1
				if stage < 1 || stage > qualificationStages { continue }
				if stage > best[branch] { best[branch] = stage }
			}
		}
		ids := make([]uint32, 0, len(best))
		for branch, stage := range best {
			ids = append(ids, uint32(branch*100+stage))
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		fields[4] = proto.TypedU32Arr("311", ids)
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
		// f7 稀有鱼排名：从稀有鱼纪录派生 obj5010（大区 id/rank/时间）。
		if recs, err := s.DB.PlayerCatchRecords(cctx, uid); err == nil {
			items := make([]*proto.Value, 0)
			rank := uint32(0)
			for _, r := range recs {
				if r.Grade < 3 { continue }
				rank++
				items = append(items, wiregen.MustMakeObj("iahhndknkbe", map[int]*proto.Value{
					0: proto.U32(uint32(1)),   // 大区 id（官服样本恒 1/3）
					1: proto.U32(rank),        // rank 序号
					2: proto.U64(unixToFiletime100ns(r.TS)),
				}))
			}
			fields[7] = proto.Args(proto.S("207"), items...)
		}
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
