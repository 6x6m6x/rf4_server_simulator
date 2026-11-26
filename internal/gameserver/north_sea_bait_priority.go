package gameserver

import (
	"context"
	"strings"
)

// 挪威海「用户爱吃饵优先」补丁（2026-11-26）。
//
// 问题：后台「鱼饵偏好」里用户点名的饵，虽然通过 fish_bait_override 覆盖了
// 内置 allow 清单，但后面还有多层内置规则会把它挡掉：
//   1) bait_exact_only 严格清单直接跳过 override；
//   2) rf4 占比分级 / 长尾闸 / 拟饵门（<1% 出池、1~5% ×0.25）；
//   3) bite_gate 的 G1 食性硬门、G-diet 天然饵双票。
// 结果是用户明明在后台设了「这条鱼爱吃这个饵」，到了挪威海却钓不出来。
//
// 本补丁只在挪威海（level_012_north_sea / 挪威海 / north_sea）生效，且只在
// 用户给该鱼点过 allow 清单时接管判鱼：
//   - 用户优先：只认 allow 清单里的饵，满档权重高于任何内置规则；
//   - 用户没指定的饵一律不吃（权重 0，不回退内置规则）；
//   - 前置条件保留：用户规则自身的 condition 照常判定，不满足则本补丁不接管
//     （回到原链路），不擅自清除后台自动带入的条件。
// 其它水域、以及没设置过用户规则的鱼，行为完全不变。

// isNorthSeaBaitLoc 判断本次判鱼是否发生在挪威海。
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

// northSeaUserBaitRule 返回挪威海用户覆盖判鱼状态：
//
//	(r, true,  hit) 用户规则生效且钩上出现用户点名的饵 → 只认用户 allow 饵
//	(r, true, !hit) 用户规则生效但钩上没有用户饵     → 该鱼一律不吃
//	(_, false, _)   用户规则不存在/前置条件不满足     → 走原内置链路
func (s *Server) northSeaUserBaitRule(ctx context.Context, c fishRollContext,
	fishZh string, baits []ruleBait, locs ...string) (fishBaitRule, bool, bool) {
	if s == nil || s.DB == nil {
		return fishBaitRule{}, false, false
	}
	sea := isNorthSeaBaitLoc(c.loc)
	for _, l := range locs {
		if isNorthSeaBaitLoc(l) {
			sea = true
		}
	}
	if !sea {
		return fishBaitRule{}, false, false
	}
	ov, ok := s.baitOverrides(ctx)[ruleName(fishZh)]
	if !ok || len(ov.rule.allow) == 0 {
		return fishBaitRule{}, false, false
	}
	r := ov.rule
	// 前置规则保留：用户规则里的 condition 原样生效；不满足则交给原链路。
	if !ruleConditionAllows(r, c) {
		return fishBaitRule{}, false, false
	}
	hit := false
	for _, b := range baits {
		if baitBlockedByRule(r, c, b) {
			continue
		}
		if baitAllowedByRule(r, b, nil) {
			hit = true
			break
		}
	}
	return r, true, hit
}
