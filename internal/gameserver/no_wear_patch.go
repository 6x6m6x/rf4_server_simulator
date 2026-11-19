package gameserver

import (
	"context"

	"rf4server/internal/store"
)

// 用户补丁：钓具全件不磨损（鱼竿与渔轮的所有部件及本体），但保留外力损失。
//
// 关闭的「渐变耐久磨损」通道：
//   - 钓获磨损：竿本体、鱼线、引线/前导、鱼钩、浮漂、拟饵/吸引成分、仿生饵
//     （catch_settlement.go / econ_market.go / progress_ops.go 的 wearPerCatch 路径）；
//   - 抛竿使用磨损（bait_consumption.go 的 baitUseRule，仅归零耐久、保留数量消耗）；
//   - 搏鱼受力磨损：摩擦片制动力、渔轮本体、轮侧零件（轮油/主齿轮/轴承/线杯）、
//     竿侧零件（竿坯/导环）、前导（reel_fight_wear.go 的 applyFightReelWear 及
//     reel_part_wear.go 的 wearPartsInFields）。
//
// 明确不影响（外力直接损失，恒按上游逻辑处理）：
//   - 断竿 rod_break、炸轮 reel_overload、断线/切线 line_break/line_cut、
//     炸钩/炸件 component_break，以及对应的 lost_on_*/detached_on_* 事件
//     ——这些全部在 gear_damage.go，不经过本补丁的任何拦截点。
//   - 饵料数量消耗（consumed）。
//
// noWearAll 是唯一总开关：true（默认）=全件不磨损；false=恢复上游原生磨损，
// 供锁定旧磨损行为的测试临时置回并 defer 恢复，以验证机制完整性。
// 注意：noWearAmount 对 line/hook/lure 的归零是原补丁既有行为，不随开关变化，
// 因此原补丁已同步过断言的测试在开关置 false 时仍成立。
//
// 本文件与配套改动构成一个独立补丁；每次更新（换基线或合并新 PR）后需重新应用。

// noWearAll 全件不磨损总开关。默认 true；测试可用 defer 临时置 false。
var noWearAll = true

// noWearItemCategories 是原补丁作用的物品类别集合（线/前导/钩/拟饵/仿生饵）。
// 全开关开启时 noWearCategory 对所有类别返回 true，此集合作为开关关闭时的基线行为。
var noWearItemCategories = map[int64]bool{
	catLine: true,
	catLead: true,
	catHook: true,
	catLure: true,
	// catBait=27：仅其中可重复使用的仿生饵（BaitType=17）有耐久磨损；
	// 消耗型鱼饵走数量扣减（consumed），补丁不改变这一点。
	catBait: true,
}

func noWearCategory(category int64) bool {
	return noWearAll || noWearItemCategories[category]
}

// noWearAmount 归零钓获磨损入口的速率。noWearAll 开启时所有 key（含竿本体
// "rod"、浮漂 "float"、其它 "default"）一律归零；关闭时保留原补丁对
// line/hook/lure 的归零基线。不修改 wearPerCatch 本身，避免影响其它读取。
func noWearAmount(key string, amount float64) float64 {
	if noWearAll {
		return 0
	}
	switch key {
	case "line", "hook", "lure":
		return 0
	default:
		return amount
	}
}

// noWearBaitWear 保留「是否消耗数量」判定，只把耐久磨损置 0。
func noWearBaitWear(consumed bool, wear float64, category int64) (bool, float64) {
	if noWearCategory(category) {
		return consumed, 0
	}
	return consumed, wear
}

// noWearLeaderStressRate 关闭前导搏鱼受力磨损；rate<=0 时
// applyLeaderStressWear 自身会早返回，不产生写库与损伤历史。
func noWearLeaderStressRate(rate float64) float64 {
	if noWearCategory(catLead) {
		return 0
	}
	return rate
}

// noWearItemRow 兜底判定：slot 非类别口径的旧数据按 item_def.Category 再判一次。
// 查不到定义时保守放行（不拦截）。
func (s *Server) noWearItemRow(ctx context.Context, row *store.Item) bool {
	if s == nil || row == nil || s.DB == nil {
		return false
	}
	if noWearAll {
		return true
	}
	def, err := s.itemDefForRow(ctx, row)
	if err != nil || def == nil {
		return false
	}
	return noWearCategory(def.Category)
}
