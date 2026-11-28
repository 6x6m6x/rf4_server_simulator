package gameserver

import (
	"context"
	"fmt"
	"strconv"
)

// 展示称号（绿字「N级XX高手」）的持久化与协议编码。
//
// 官服抓包 2026-11-25 实包（24/19 social/members → dgbmlekfpbg.f8 / pfcgjmifmnd）：
//   senlin520     f8=262151  = 0x40007 → 分支4 等级7
//   Shang Da Yu   f8=393218  = 0x60002 → 分支6 等级2
//   GOF_YANG      f8=393218  = 0x60002
//   其它样本       f8=0x10001/0x40004/0x80003/0x140007/0x130007
// 编码明确为 (资格分支ID<<16) | 该分支连续通过等级，与 24/18.f7 的
// obj5010(f0=分支ID, f1=等级) 同源。客户端据此在昵称下方渲染绿字称号。
//
// 服务端此前在 hSocialUserCard 把 f8 恒填 0 —— 别人资料卡没有绿字的根因。
// 玩家「切换称号」的选择存 kv(qual-title:<uid>=分支ID)，未设置时按玩家
// 已通过资格里「等级最高、同级分支最大」自动选一个，避免恒落到分支1
// （纺车竿）——这就是每次重登都变回「7级纺车竿高手」的根因。

func qualTitleKey(uid int64) string {
	return fmt.Sprintf("qual-title:%d", uid)
}

// qualTitleValue 计算要下发的 f8：(branch<<16)|stage。无资格返回 0。
func qualTitleValue(quals []profileQualification, chosenBranch int64) uint32 {
	if len(quals) == 0 {
		return 0
	}
	// 玩家显式选择的分支：只在他确实通过该分支时生效。
	if chosenBranch > 0 {
		for _, q := range quals {
			if q.branch == chosenBranch && q.stage > 0 {
				return uint32(q.branch)<<16 | uint32(q.stage)
			}
		}
	}
	// 未设置：等级最高优先，同级分支大者优先（不再恒选分支1）。
	best := quals[0]
	for _, q := range quals[1:] {
		if q.stage > best.stage || (q.stage == best.stage && q.branch > best.branch) {
			best = q
		}
	}
	if best.stage <= 0 {
		return 0
	}
	return uint32(best.branch)<<16 | uint32(best.stage)
}

// currentQualTitle 读玩家称号：kv 里存的分支 + 实时资格状态。
func (s *Server) currentQualTitle(ctx context.Context, uid int64) uint32 {
	if s == nil || s.DB == nil || uid <= 0 {
		return 0
	}
	chosen := int64(0)
	if raw, ok, err := s.DB.KVGet(ctx, qualTitleKey(uid)); err == nil && ok {
		if n, e := strconv.ParseInt(raw, 10, 64); e == nil {
			chosen = n
		}
	}
	states, err := s.DB.QualificationStates(ctx, uid)
	if err != nil {
		return 0
	}
	return qualTitleValue(collectProfileQualifications(states), chosen)
}

// setQualTitle 保存玩家选择的展示分支；branch<=0 清除（回到自动选择）。
func (s *Server) setQualTitle(ctx context.Context, uid, branch int64) error {
	if s == nil || s.DB == nil || uid <= 0 {
		return fmt.Errorf("参数错误")
	}
	if branch <= 0 {
		return s.DB.KVSet(ctx, qualTitleKey(uid), "0")
	}
	// 只接受玩家已通过的分支，防止下发不存在的称号。
	states, err := s.DB.QualificationStates(ctx, uid)
	if err != nil {
		return err
	}
	for _, q := range collectProfileQualifications(states) {
		if q.branch == branch && q.stage > 0 {
			return s.DB.KVSet(ctx, qualTitleKey(uid), strconv.FormatInt(branch, 10))
		}
	}
	return fmt.Errorf("该玩家尚未通过此资格分支")
}

// qualTitleOfItemID 供手工排查：资格奖杯 item_id → 分支ID（非资格杯返回 0）。
func qualTitleOfItemID(itemID string) int64 {
	for branch, cup := range qualificationCupByBranch {
		if cup.ItemID == itemID {
			return branch
		}
	}
	return 0
}
