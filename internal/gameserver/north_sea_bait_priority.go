package gameserver

import (
	"context"
	"strings"
	"time"
)

// 用户爱吃饵优先（2026-11-28 彻底重写）。
//
// 一条鱼能不能咬，分两级、顺序固定：
//   第一级 区域限制（淡水 = fish_zone 落点名册；挪威海 = 用户规则的海图区号）
//   第二级 饵命中（用户点名的 allow / 轮换组）
//
// ★淡水与挪威海的区域是两套完全不同的东西，不能混用：
//   - 挪威海区域：用户饵规则 condition 里的海图/声呐区号（chartZones/sonarZones），
//     沿中性语义 ruleConditionAllows 判定，不满足回退内置链路。
//   - 淡水区域：后台「鱼种区域名册」fish_zone（grid_rect/grid_cells/circle 落点坐标）。
//     被独占区名单限定的鱼种，落点不在包含它的独占区内 → 判定区域外，直接剔除，
//     绝不让用户饵权把它从区外钓出来。
//
// 本文件只管「用户设过喜爱饵」的鱼；没设过的鱼完全走内置链路，不受影响。

type userBaitVerdict int

const (
	userBaitNone    userBaitVerdict = iota // 用户没设规则 / 前置条件不满足 → 走内置链路
	userBaitBlocked                        // 区域限制挡下（淡水 fish_zone 区外）→ 剔除该鱼
	userBaitMiss                           // 用户规则生效但钩上没用户点名的饵 → 不吃
	userBaitHit                            // 命中用户饵 → 满权重、跳过内置分级/硬门
)

func isNorthSeaBaitLoc(locs ...string) bool {
	for _, loc := range locs {
		if loc == "" {
			continue
		}
		switch loc {
		case "level_012_north_sea", "north_sea", "挪威海":
			return true
		}
		if strings.Contains(loc, "north_sea") {
			return true
		}
	}
	return false
}

// freshwaterZoneBlocksFish 判断淡水 fish_zone 名册是否把这条鱼挡在落点之外。
//
// 语义（只针对「用户设了喜爱饵」的鱼）：
//   若该鱼种出现在任一启用的 exclusive 独占区名单里，且落点不在**任何**
//   包含它的独占区内 → 区域外，挡下。
//   落点命中了某个包含它的独占区 → 放行（区域内可咬）。
//   没有独占区名单包含这条鱼 → 不涉及区域限制，放行。
//
// 注意：这不动 fish_zone 既有「独占区命中区内只出名单鱼、区外不受影响」的
// 全局语义；只给「用户设了喜爱饵的鱼」补一条区外约束，堵住用户饵权突破区域。
func (s *Server) freshwaterZoneBlocksFish(ctx context.Context, statLoc, fishKey string, c fishRollContext) bool {
	if s == nil || s.DB == nil || statLoc == "" || fishKey == "" {
		return false
	}
	zones := s.fishZonesFor(ctx, statLoc)
	listed := false // 该鱼是否被某个独占区名单收编
	for _, z := range zones {
		if !z.Enabled || z.Mode != "exclusive" {
			continue
		}
		for _, f := range z.Fish {
			if f.Enabled && f.Weight > 0 && f.FishKey == fishKey {
				listed = true
				// 落点命中了包含本鱼的独占区 → 区域内，放行。
				if fishZoneContains(z, c) {
					return false
				}
				break
			}
		}
	}
	// 被独占区收编，但落点不在任何包含它的独占区 → 区域外。
	return listed
}

// evaluateUserBait 是用户爱吃饵的唯一判定入口，返回 (规则, 轮换, 判定)。
// statLoc 是解析后的水域名，用于淡水 fish_zone 名册查询（挪威海可传空）。
func (s *Server) evaluateUserBait(ctx context.Context, c fishRollContext, statLoc, fishKey,
	fishZh string, baits []ruleBait, now time.Time) (fishBaitRule, baitRotation, userBaitVerdict) {
	if s == nil || s.DB == nil {
		return fishBaitRule{}, baitRotation{}, userBaitNone
	}
	key := ruleName(fishZh)
	ov, ok := s.baitOverrides(ctx)[key]
	if !ok || len(ov.rule.allow) == 0 {
		return fishBaitRule{}, baitRotation{}, userBaitNone
	}
	r := ov.rule

	// 第一级：区域限制。淡水走 fish_zone 落点名册，挪威海走海图区号。
	if isNorthSeaBaitLoc(c.loc, statLoc) {
		// 挪威海：condition 里的海图区号不满足 → 回退内置链路（原语义）。
		if !ruleConditionAllows(r, c) {
			return fishBaitRule{}, baitRotation{}, userBaitNone
		}
	} else {
		// 淡水：fish_zone 独占区名单鱼在落点区外 → 挡下，不回退内置链路。
		if s.freshwaterZoneBlocksFish(ctx, statLoc, fishKey, c) {
			return r, ov.rot, userBaitBlocked
		}
		// 淡水的非区域前置（船/路亚运动等）沿用中性语义。
		rNoZone := r
		rNoZone.chartAny = false
		rNoZone.chartZones = nil
		rNoZone.sonarZones = nil
		if !ruleConditionAllows(rNoZone, c) {
			return fishBaitRule{}, baitRotation{}, userBaitNone
		}
	}

	// 第二级：饵命中。带上当前轮换组，后台同时设 allow 与轮换组时组内饵也算命中。
	activeGroup := activeBaitGroup(ov.rot, now)
	for _, b := range baits {
		if baitBlockedByRule(r, c, b) {
			continue
		}
		if baitAllowedByRule(r, b, activeGroup) {
			return r, ov.rot, userBaitHit
		}
	}
	return r, ov.rot, userBaitMiss
}
