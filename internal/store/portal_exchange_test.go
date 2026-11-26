package store

import (
	"context"
	"path/filepath"
	"testing"
)

// 银币兑换金币：非会员 2000000 银分 → 100 金分；会员 1800000 → 100。
func TestPortalSilverToGoldRates(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "exchange.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(); err != nil { t.Fatal(err) }
	if _, err := db.EnsureProfile(ctx, 900); err != nil { t.Fatal(err) }
	now := int64(1700000000)
	// 造 400 万银分
	if _, err := db.SQL.ExecContext(ctx, `UPDATE profile SET silver=4000000,gold=0 WHERE uid=900`); err != nil { t.Fatal(err) }

	// 非会员：1 份 = 2000000 → 100 金
	r, err := db.PortalSilverToGold(ctx, 900, 1, now)
	if err != nil { t.Fatal(err) }
	if r.SilverSpent != 2000000 || r.GoldGained != 100 || r.Premium { t.Fatalf("非会员兑换=%+v", r) }
	if r.SilverMinor != 2000000 || r.GoldMinor != 100 { t.Fatalf("余额=%+v", r) }

	// 会员：1200000 银分 → 1 份需 1800000，余额不足
	if _, err := db.SQL.ExecContext(ctx, `UPDATE profile SET membership=1,membership_exp=? WHERE uid=900`, now+86400); err != nil { t.Fatal(err) }
	if _, err := db.PortalSilverToGold(ctx, 900, 2, now); err == nil { t.Fatal("余额不足应报错") }

	// 会员 1 份 = 1800000 → 100 金（倍率 +0.2）
	r2, err := db.PortalSilverToGold(ctx, 900, 1, now)
	if err != nil { t.Fatal(err) }
	if r2.SilverSpent != 1800000 || !r2.Premium || r2.GoldGained != 100 { t.Fatalf("会员兑换=%+v", r2) }
}
