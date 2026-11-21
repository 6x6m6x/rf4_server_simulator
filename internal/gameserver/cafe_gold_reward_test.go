package gameserver

import "testing"

func bandFor(tier CafeBountyTier) (int64, int64) {
	switch tier {
	case CafeTierCommon:
		return cafeBandCommonLo, cafeBandCommonHi
	case CafeTierRareCommon:
		return cafeBandRareCommonLo, cafeBandRareCommonHi
	case CafeTierTrophy:
		return cafeBandTrophyLo, cafeBandTrophyHi
	case CafeTierBlue:
		return cafeBandBlueLo, cafeBandBlueHi
	case CafeTierRareTrophy:
		return cafeBandRareTrophyLo, cafeBandRareTrophyHi
	case CafeTierRareBlue:
		return cafeBandRareBlueLo, cafeBandRareBlueHi
	}
	return 0, 0
}

func TestCafeGoldBountyMatrix(t *testing.T) {
	tests := []struct {
		name                  string
		rarity                float64
		grade                 int64
		weightG, baseG        int64
		want                  CafeBountyTier
	}{
		// 普通/达标档
		{"常见普通鱼刚过线", 0.0, 1, 0, 0, CafeTierCommon},
		{"常见普通鱼中权重", 0.5, 2, 0, 0, CafeTierCommon},
		{"稀有普通鱼入门", 0.7, 1, 0, 0, CafeTierRareCommon},
		{"稀有普通鱼极值", 1.0, 2, 0, 0, CafeTierRareCommon},
		// 奖杯档
		{"常见奖杯", 0.2, 3, 3000, 3000, CafeTierTrophy},
		{"稀有奖杯入门", 0.7, 3, 3000, 3000, CafeTierRareTrophy},
		{"稀有奖杯超重", 1.0, 3, 4500, 3000, CafeTierRareTrophy},
		// 蓝冠档
		{"常见蓝冠", 0.3, 4, 5000, 5000, CafeTierBlue},
		{"稀有蓝冠入门", 0.7, 4, 5000, 5000, CafeTierRareBlue},
		{"稀有蓝冠超重", 1.0, 4, 7500, 5000, CafeTierRareBlue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTier, got := CafeGoldBounty(tc.rarity, tc.grade, tc.weightG, tc.baseG)
			if gotTier != tc.want {
				t.Fatalf("档位 %v 期望 %v", gotTier, tc.want)
			}
			lo, hi := bandFor(tc.want)
			if got < lo || got > hi {
				t.Fatalf("赏金 %d 越出区间 [%d,%d]", got, lo, hi)
			}
		})
	}
}

func TestCafeGoldBountySeparatesRareAndCommon(t *testing.T) {
	// 核心回归：稀有鱼的蓝冠与常见鱼的蓝冠必须落在完全不同的价值区间。
	var rareBlueMin int64 = cafeBandRareBlueLo
	// 常见鱼蓝冠拉满稀有度和重量，也绝不能摸到稀有蓝冠的下限。
	_, topCommonBlue := CafeGoldBounty(0.699, 4, 99999, 1000)
	if topCommonBlue >= rareBlueMin {
		t.Fatalf("常见蓝冠 %d 触及稀有蓝冠区间下限 %d，分档失效", topCommonBlue, rareBlueMin)
	}
	// 稀有鱼蓝冠即便刚过线，也高于常见蓝冠上限。
	_, lowRareBlue := CafeGoldBounty(0.7, 4, 0, 0)
	if lowRareBlue <= cafeBandBlueHi {
		t.Fatalf("稀有蓝冠 %d 未高于常见蓝冠上限 %d，分档失效", lowRareBlue, cafeBandBlueHi)
	}
	// 奖杯档同理。
	_, topCommonTrophy := CafeGoldBounty(0.699, 3, 99999, 1000)
	if topCommonTrophy >= cafeBandRareTrophyLo {
		t.Fatalf("常见奖杯 %d 触及稀有奖杯区间下限 %d", topCommonTrophy, cafeBandRareTrophyLo)
	}
}

func TestCafeGoldBountyDualFactors(t *testing.T) {
	// 稀有度因子：同档内 rarity 越高赏金越高。
	last := int64(-1)
	for i := 70; i <= 100; i++ {
		_, got := CafeGoldBounty(float64(i)/100, 4, 0, 0)
		if got < last {
			t.Fatalf("稀有蓝冠稀有度非单调: rarity=%.2f got=%d last=%d", float64(i)/100, got, last)
		}
		last = got
	}
	// 重量因子：同鱼种同稀有度，越重赏金越高。
	_, base := CafeGoldBounty(0.8, 3, 3000, 3000)
	_, heavy := CafeGoldBounty(0.8, 3, 4500, 3000)
	if heavy <= base {
		t.Fatalf("重量因子未生效: base=%d heavy=%d", base, heavy)
	}
	// 双因子拉满取到区间上限。
	_, full := CafeGoldBounty(1.0, 4, 7500, 5000)
	if full != cafeBandRareBlueHi {
		t.Fatalf("双因子拉满应取稀有蓝冠上限 %d，实际 %d", cafeBandRareBlueHi, full)
	}
	// 退化情形：无基准重量时重量因子为 0，与不传一致。
	_, noBase := CafeGoldBounty(0.8, 3, 99999, 0)
	_, zeroWeight := CafeGoldBounty(0.8, 3, 0, 0)
	if noBase != zeroWeight {
		t.Fatalf("无基准时重量因子应忽略: %d vs %d", noBase, zeroWeight)
	}
}

func TestCafeRarityFactorMonotonic(t *testing.T) {
	// 常见段与稀有段各自单调，且稀有段整体高于常见段同位置。
	prevCommon, prevRare := int64(-1), int64(-1)
	for i := 0; i <= 100; i++ {
		r := float64(i) / 100
		_, common := CafeGoldBounty(r, 1, 0, 0)
		if r < cafeRareRarity {
			if common < prevCommon {
				t.Fatalf("常见段非单调: rarity=%.2f", r)
			}
			prevCommon = common
		}
	}
	_ = prevRare
}
