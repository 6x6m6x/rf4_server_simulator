package gameserver

import (
	"context"
	"strings"
	"time"
)

// 用户爱吃饵优先补丁（2026-11-26 挪威海，2026-11-27 扩展到全部水域）。
//
// 问题：后台「鱼饵偏好」里用户点名的饵，虽然通过 fish_bait_override 覆盖了
// 内置 allow 清单，但后面还有多层内置规则会把它挡掉：
//   1) bait_exact_only 严格清单直接跳过 override；
//   2) rf4 占比分级 / 长尾闸 / 拟饵门（<1% 出池、1~5% ×0.25）；
//   3) bite_gate 的 G1 食性硬门、G-diet 天然饵双票。
//
// 判定规则：
//   - 全部水域：用户给某鱼点过 allow 清单时，饵权层只认用户点名的饵，
//     满档权重且跳过严格清单/占比分级/长尾闸/拟饵门；用户没点名的饵一律
//     不吃（不回退内置规则）。用户未设置过的鱼沿用内置规则。
//   - 仅挪威海：额外豁免 G1/G-diet 食性硬门（挪威海全豁免）。
//     其它水域只改饵权，硬门仍按内置规则判定。
//   - 前置条件（condition）在所有水域都保留：用户规则自身 condition 原样判，
//     不满足则补丁不接管，回到原链路。

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

// userBaitRuleForFish 返回用户覆盖判鱼状态：
//
//	(r, true,  hit) 用户规则生效且钩上出现用户点名的饵 → 只认用户 allow 饵
//	(r, true, !hit) 用户规则生效但钩上没有用户饵     → 该鱼一律不吃
//	(_, false, _)   用户规则不存在/前置条件不满足     → 走原内置链路
func (s *Server) userBaitRuleForFish(ctx context.Context, c fishRollContext,
	fishZh string, baits []ruleBait, now time.Time) (fishBaitRule, bool, bool) {
	if s == nil || s.DB == nil {
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
	// 命中判定要带上当前轮换组：后台同时设置 allow 与轮换组时，
	// 组内饵同样算「用户点名」，否则严格轮换组会被误判成没命中。
	activeGroup := activeBaitGroup(ov.rot, now)
	hit := false
	for _, b := range baits {
		if baitBlockedByRule(r, c, b) {
			continue
		}
		if baitAllowedByRule(r, b, activeGroup) {
			hit = true
			break
		}
	}
	return r, true, hit
}
