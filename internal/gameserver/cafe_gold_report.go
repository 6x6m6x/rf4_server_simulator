package gameserver

import (
	"context"
	"sort"
	"time"

	"rf4server/internal/store"
)

// CafeBountyGroup 单个咖啡厅（水域）的悬赏快照。
type CafeBountyGroup struct {
	Level    string
	Loc      string
	Slots    []CafeBountySlot
}

// CafeBountySlot 一个订单槽位的可视化明细。
type CafeBountySlot struct {
	Ordinal      int
	Species      string
	SpeciesZH    string
	Grade        int64
	GradeZH      string
	Rarity       float64 // 0=常见 1=稀有
	RareFish     bool    // 是否稀有鱼种（白名单 rare/giant 或 rarity>=0.7）
	GiantFish    bool    // 是否巨物白名单
	WeightFac    float64 // 重量比因子 0~1
	CatchWt      float64
	MinWeightG   int64
	Quantity     int64
	Tier         CafeBountyTier
	Gold         int64 // 金币分（100=1 金）
	SubsidyCents int64 // 巨物补贴（金币分）
	PeriodMin    int64
}

// poolRowTier 由候选行的档位与稀缺映射到六档枚举。
func poolRowTier(grade int64, isRare bool) CafeBountyTier {
	if grade >= 4 {
		if isRare {
			return CafeTierRareBlue
		}
		return CafeTierBlue
	}
	if grade == 3 {
		if isRare {
			return CafeTierRareTrophy
		}
		return CafeTierTrophy
	}
	if isRare {
		return CafeTierRareCommon
	}
	return CafeTierCommon
}

var cafeGradeZH = map[int64]string{1: "普通", 2: "达标", 3: "奖杯", 4: "蓝冠"}

// BuildCafeBountyReport 构建全部咖啡厅的悬赏快照。
// 优先从预生成候选池读取真实行（含白名单/补贴标记），池未生成时回退旧订单逻辑。
func BuildCafeBountyReport(ctx context.Context, srv *Server, levels []string, now int64) ([]CafeBountyGroup, error) {
	var groups []CafeBountyGroup
	for _, level := range levels {
		loc := srv.DB.ResolveStatLoc(ctx, level)
		g := CafeBountyGroup{Level: level, Loc: loc}
		poolRows, err := srv.DB.CafeBountyPoolByLevel(ctx, level)
		if err != nil {
			return nil, err
		}
		if len(poolRows) > 0 {
			// 每个槽位取当前周期的候选（与运行时 cafeTemplatesFromPool 一致）。
			bySlot := make(map[int][]store.CafeBountyPoolRow)
			for _, r := range poolRows {
				bySlot[r.Slot] = append(bySlot[r.Slot], r)
			}
			slots := make([]int, 0, len(bySlot))
			for slot := range bySlot {
				slots = append(slots, slot)
			}
			sort.Ints(slots)
			for _, slot := range slots {
				cands := bySlot[slot]
				if len(cands) == 0 {
					continue
				}
				period, phase := cands[0].PeriodSeconds, cands[0].PhaseSeconds
				cycle := int64(0)
				if now >= phase {
					cycle = (now - phase) / period
				}
				r := cands[int(cycle%int64(len(cands)))]
				var weightFac float64
				// 池行已按档位线定重，重量比因子反映在赏金中；展示用 rarity 段内位置近似。
				weightFac = 0
				catchWt := queryCatchWeight(ctx, srv.DB, loc, r.Species)
				qty := r.Quantity
				if qty < 1 {
					qty = 1
				}
				g.Slots = append(g.Slots, CafeBountySlot{
					Ordinal: slot, Species: r.Species, SpeciesZH: r.NameZH,
					Grade: r.Grade, GradeZH: cafeGradeZH[r.Grade],
					Rarity: r.Rarity, RareFish: r.IsRare, GiantFish: r.IsGiant,
					WeightFac: weightFac, CatchWt: catchWt,
					MinWeightG: r.WeightG, Quantity: qty,
					Tier: poolRowTier(r.Grade, r.IsRare), Gold: r.GoldCents, SubsidyCents: r.SubsidyCents,
					PeriodMin: period / 60,
				})
			}
			groups = append(groups, g)
			continue
		}
		// 回退：旧订单逻辑（池未生成时）。
		templates, err := srv.cafeTemplates(ctx, level, now)
		if err != nil || len(templates) == 0 {
			continue
		}
		rarityMap, err := CafeRarityMap(ctx, srv.DB, level)
		if err != nil {
			return nil, err
		}
		for i, tpl := range templates {
			v, t, b := srv.DB.GradeLines(ctx, tpl.Species)
			grade := gradeFromLines(v, t, b, tpl.MinWeightG)
			rarity := rarityMap[tpl.Species]
			baseG := cafeBandBaseG(grade, v, t, b)
			tier, _ := CafeGoldBounty(rarity, grade, tpl.MinWeightG, baseG)
			var weightFac float64
			if baseG > 0 && tpl.MinWeightG > baseG {
				weightFac = clamp01((float64(tpl.MinWeightG)/float64(baseG) - 1) / cafeBountyWeightSpan)
			}
			zh := queryFishName(ctx, srv.DB, tpl.Species)
			catchWt := queryCatchWeight(ctx, srv.DB, loc, tpl.Species)
			g.Slots = append(g.Slots, CafeBountySlot{
				Ordinal: i, Species: tpl.Species, SpeciesZH: zh,
				Grade: grade, GradeZH: cafeGradeZH[grade],
				Rarity: rarity, RareFish: rarity >= cafeRareRarity, WeightFac: weightFac, CatchWt: catchWt,
				MinWeightG: tpl.MinWeightG, Quantity: tpl.Quantity,
				Tier: tier, Gold: tpl.RewardCents,
				PeriodMin: tpl.PeriodSeconds / 60,
			})
		}
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Level < groups[j].Level })
	return groups, nil
}

func queryFishName(ctx context.Context, db *store.DB, fishKey string) string {
	var name string
	_ = db.SQL.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(name_zh,''),name) FROM item_catalog WHERE item_id=?`, fishKey).Scan(&name)
	if name == "" {
		return fishKey
	}
	return name
}

func queryCatchWeight(ctx context.Context, db *store.DB, loc, fishKey string) float64 {
	var wt float64
	_ = db.SQL.QueryRowContext(ctx, `SELECT COALESCE(l.catches,1) FROM loc_wiki_fish w LEFT JOIN loc_fish l ON l.loc=w.loc AND l.fish_key=w.fish_key WHERE w.loc=? AND w.fish_key=?`, loc, fishKey).Scan(&wt)
	return wt
}

// CafeWaterLevels 返回有悬赏模板的全部水域 level key。
func CafeWaterLevels() []string {
	cafeRewards.Do(loadCafeRewards)
	levels := make([]string, 0, len(cafeRewards.Waters))
	for k := range cafeRewards.Waters {
		levels = append(levels, k)
	}
	sort.Strings(levels)
	return levels
}

var _ = time.Now
