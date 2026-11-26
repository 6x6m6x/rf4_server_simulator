package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// 会员在个人中心的金币消费享 7 折：改名 5000→3500 金分。
func TestPortalCustomizePremiumDiscount(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "premium.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if err := db.Migrate(); err != nil { t.Fatal(err) }
	if _, err := db.EnsureProfile(ctx, 901); err != nil { t.Fatal(err) }
	now := time.Now().Unix()
	if _, err := db.SQL.ExecContext(ctx, `UPDATE profile SET gold=10000 WHERE uid=901`); err != nil { t.Fatal(err) }

	// 非会员：扣 5000
	if err := db.PortalRename(ctx, 901, "普通玩家", now); err != nil { t.Fatal(err) }
	var gold int64
	db.SQL.QueryRowContext(ctx, `SELECT gold FROM profile WHERE uid=901`).Scan(&gold)
	if gold != 5000 { t.Fatalf("非会员改名后余额=%d 期望5000", gold) }

	// 会员：扣 3500
	db.SQL.ExecContext(ctx, `UPDATE profile SET gold=10000,membership=1,membership_exp=? WHERE uid=901`, now+86400)
	if err := db.PortalRename(ctx, 901, "会员玩家", now); err != nil { t.Fatal(err) }
	db.SQL.QueryRowContext(ctx, `SELECT gold FROM profile WHERE uid=901`).Scan(&gold)
	if gold != 6500 { t.Fatalf("会员改名后余额=%d 期望6500(扣3500)", gold) }

	// Status 也返回会员价
	_, price, err := db.PortalCustomizeStatus(ctx, 901)
	if err != nil { t.Fatal(err) }
	if price != PortalCustomizePriceGoldPremium { t.Fatalf("会员单价=%d 期望%d", price, PortalCustomizePriceGoldPremium) }
}
