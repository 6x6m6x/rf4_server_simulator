package gameserver

import (
	"context"
	"math"
	"sort"

	"rf4server/internal/store"
)

// 咖啡厅赏金金币化：内部仍按“分”存储，100 分 = 1 金币（与 loot_box 金币口径一致）。
//
// 二维矩阵定价：重量档（普通/达标、奖杯、蓝冠）× 鱼种稀缺（常见、稀有）共 6 格，
// 每格独立金币区间。核心目的：把“常见鱼的蓝冠”和“稀有鱼的蓝冠”彻底分开——
// 前者是同种鱼养到极致的重量成就，后者是稀缺鱼种的重量成就，价值本就不同。
//
// 区间（金币分）：
//
//	普通/达标  常见 0.5~5 金   稀有 3~8 金
//	奖杯       常见 5~15 金   稀有 25~40 金
//	蓝冠       常见 12~25 金  稀有 35~50 金
const (
	CafeGoldPricingVersion = 4 // v4：六档二维矩阵（v3 为三档金币版，旧单需失效重建）

	cafeRareRarity = 0.7 // 权重排名落在最稀疏的 30% 视为“稀有鱼种”

	// 六档区间下限/上限（金币分）。
	cafeBandCommonLo, cafeBandCommonHi     = 50, 500    // 0.5~5 金
	cafeBandRareCommonLo, cafeBandRareCommonHi = 300, 800 // 3~8 金
	cafeBandTrophyLo, cafeBandTrophyHi     = 500, 1500   // 5~15 金
	cafeBandRareTrophyLo, cafeBandRareTrophyHi = 2500, 4000 // 25~40 金
	cafeBandBlueLo, cafeBandBlueHi         = 1200, 2500  // 12~25 金
	cafeBandRareBlueLo, cafeBandRareBlueHi = 3500, 5000  // 35~50 金

	// 双因子权重：稀有度为主（鱼种稀缺是用户核心诉求），重量比为加成。
	cafeBountyRarityWeight = 0.65
	cafeBountyWeightWeight = 0.35
	// 重量达到档位基准 1.5 倍时重量因子取满。
	cafeBountyWeightSpan = 0.5
)

// CafeBountyTier 咖啡厅悬赏梯度（按赏金价值递增排列，前端展示顺序与此一致）。
type CafeBountyTier int

const (
	CafeTierCommon     CafeBountyTier = iota // 常见普通鱼 0.5~5 金
	CafeTierRareCommon                       // 稀有普通鱼 3~8 金
	CafeTierTrophy                           // 常见奖杯 5~15 金
	CafeTierBlue                             // 常见蓝冠 12~25 金
	CafeTierRareTrophy                       // 稀有奖杯 25~40 金
	CafeTierRareBlue                         // 稀有蓝冠 35~50 金
)

func (t CafeBountyTier) String() string {
	switch t {
	case CafeTierRareCommon:
		return "稀有普通鱼"
	case CafeTierTrophy:
		return "奖杯鱼"
	case CafeTierBlue:
		return "蓝冠鱼"
	case CafeTierRareTrophy:
		return "稀有奖杯鱼"
	case CafeTierRareBlue:
		return "稀有蓝冠鱼"
	default:
		return "普通鱼"
	}
}

// CafeRarityMap 依据地图鱼种捕获权重计算稀有度：1 = 最稀有（捕获权重最低），
// 0 = 最常见（权重最高）。相同权重的鱼种取并列平均秩，避免同权重被人为拉开差距。
func CafeRarityMap(ctx context.Context, db *store.DB, level string) (map[string]float64, error) {
	roster, err := db.LocRoster(ctx, db.ResolveStatLoc(ctx, level))
	if err != nil {
		return nil, err
	}
	type entry struct {
		key     string
		catches float64
	}
	rows := make([]entry, 0, len(roster))
	for _, r := range roster {
		rows = append(rows, entry{r.FishKey, r.Catches})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].catches != rows[j].catches {
			return rows[i].catches < rows[j].catches
		}
		return rows[i].key < rows[j].key
	})
	out := make(map[string]float64, len(rows))
	if len(rows) == 1 {
		out[rows[0].key] = 0.5
		return out, nil
	}
	denom := float64(len(rows) - 1)
	for i := 0; i < len(rows); {
		j := i + 1
		for j < len(rows) && rows[j].catches == rows[i].catches {
			j++
		}
		avgRank := float64(i+j-1) / 2
		rarity := 1 - avgRank/denom
		for k := i; k < j; k++ {
			out[rows[k].key] = rarity
		}
		i = j
	}
	return out, nil
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// cafeBandBaseG 取该重量档的基准重量：普通/达标用达标线、奖杯用奖杯线、蓝冠用蓝冠线。
// 重量相对基准的倍数构成“重量比因子”，让同种鱼里养得更重的个体拿到更高赏金。
func cafeBandBaseG(grade int64, val, tro, sup *int64) int64 {
	if grade >= 4 && sup != nil && *sup > 0 {
		return *sup
	}
	if grade == 3 && tro != nil && *tro > 0 {
		return *tro
	}
	if val != nil && *val > 0 {
		return *val
	}
	return 0
}

// CafeGoldBounty 按鱼种稀有度、重量档、订单重量给出悬赏梯度和金币分。
//
// 分档：先按 grade 分重量档，再按 rarity>=0.7 分常见/稀有，落入二维矩阵 6 格之一。
// 插值：区间内由双因子决定位置——稀有度因子（本格内归一化）+ 重量比因子
// （weightG/baseG 相对 1 的超出程度），加权求和后 clamp 到 [0,1]。
// 这样“稀有鱼 + 超重个体”能顶到该档上限（稀有蓝冠 50 金），
// 而“常见鱼 + 刚过线”取该档下限。
//
// weightG/baseG 任一 <=0 时重量因子视为 0（退化成单稀有度因子）。
func CafeGoldBounty(rarity float64, grade, weightG, baseG int64) (CafeBountyTier, int64) {
	rarity = clamp01(rarity)

	// 1) 二维矩阵选档：重量档 × 鱼种稀缺。
	var tier CafeBountyTier
	var lo, hi int64
	rare := rarity >= cafeRareRarity
	switch {
	case grade >= 4: // 蓝冠
		if rare {
			tier, lo, hi = CafeTierRareBlue, cafeBandRareBlueLo, cafeBandRareBlueHi
		} else {
			tier, lo, hi = CafeTierBlue, cafeBandBlueLo, cafeBandBlueHi
		}
	case grade == 3: // 奖杯
		if rare {
			tier, lo, hi = CafeTierRareTrophy, cafeBandRareTrophyLo, cafeBandRareTrophyHi
		} else {
			tier, lo, hi = CafeTierTrophy, cafeBandTrophyLo, cafeBandTrophyHi
		}
	default: // 普通 / 达标
		if rare {
			tier, lo, hi = CafeTierRareCommon, cafeBandRareCommonLo, cafeBandRareCommonHi
		} else {
			tier, lo, hi = CafeTierCommon, cafeBandCommonLo, cafeBandCommonHi
		}
	}

	// 2) 稀有度因子：在本格所属 rarity 段内归一化到 [0,1]。
	var rarityFactor float64
	if rare {
		rarityFactor = (rarity - cafeRareRarity) / (1 - cafeRareRarity)
	} else {
		rarityFactor = rarity / cafeRareRarity
	}
	rarityFactor = clamp01(rarityFactor)

	// 3) 重量比因子：达到基准 1 倍取 0，超出 1.5 倍取满。
	var weightFactor float64
	if baseG > 0 && weightG > baseG {
		weightFactor = clamp01((float64(weightG)/float64(baseG) - 1) / cafeBountyWeightSpan)
	}

	// 4) 双因子插值到区间内。
	t := clamp01(cafeBountyRarityWeight*rarityFactor + cafeBountyWeightWeight*weightFactor)
	return tier, lo + int64(math.Round(t*float64(hi-lo)))
}

// CafeBountyRow 一行可展示的悬赏明细，供可视化导出复用。
type CafeBountyRow struct {
	Species         string
	SpeciesZH       string
	MinWeightG      int64
	Quantity        int64
	Grade           int64
	CatchWeight     float64
	Rarity          float64
	Tier            CafeBountyTier
	RewardGoldCents int64
}

// cafeBountyFor 计算某鱼种在某重量档的悬赏；rarityMap 缺失该鱼种时按中等稀有度处理。
func cafeBountyFor(ctx context.Context, db *store.DB, rarityMap map[string]float64, species string, minWeightG, quantity int64) (CafeBountyRow, error) {
	v, t, b := db.GradeLines(ctx, species)
	grade := gradeFromLines(v, t, b, minWeightG)
	rarity := 0.5
	if r, ok := rarityMap[species]; ok {
		rarity = r
	}
	baseG := cafeBandBaseG(grade, v, t, b)
	tier, cents := CafeGoldBounty(rarity, grade, minWeightG, baseG)
	return CafeBountyRow{
		Species: species, MinWeightG: minWeightG, Quantity: quantity,
		Grade: grade, Rarity: rarity, Tier: tier, RewardGoldCents: cents,
	}, nil
}
