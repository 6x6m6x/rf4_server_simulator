package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PortalCustomizePriceGold 服务中心改名/改头像单价：5000 金分（= 50 金币）。
const PortalCustomizePriceGold = 5000

// PortalCustomizePriceGoldPremium 高级会员 7 折价：3500 金分（= 35 金币）。
const PortalCustomizePriceGoldPremium = 3500

// portalCustomizePrice 按会员状态返回实际单价（会员 7 折）。
func portalCustomizePrice(premium bool) int64 {
	if premium {
		return PortalCustomizePriceGoldPremium
	}
	return PortalCustomizePriceGold
}

// PortalRename 玩家在服务中心改名：扣 5000 金分（50 金币），写新昵称与流水。
// 名称限制由调用方校验；这里再兜底一次非空与长度。
func (d *DB) PortalRename(ctx context.Context, uid int64, name string, now int64) error {
	if uid <= 0 || len([]rune(name)) == 0 || len([]rune(name)) > 24 {
		return fmt.Errorf("昵称需为 1~24 个字符")
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var gold, membership, membershipExp int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(gold,0),COALESCE(membership,0),COALESCE(membership_exp,0) FROM profile WHERE uid=?`, uid).
		Scan(&gold, &membership, &membershipExp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("角色不存在")
		}
		return err
	}
	price := portalCustomizePrice(MembershipAt(membership, membershipExp, now).Active)
	if gold < price {
		return fmt.Errorf("金币不足，需要 %d 金分（%d 金币）", price, price/100)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE profile SET name=?,gold=gold-?,updated_ts=? WHERE uid=?`,
		name, price, float64(now), uid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO txn(uid,ts,kind,item_id,gold_delta,note) VALUES(?,?,'portal_rename','',?,'portal_customize')`,
		uid, now, -price); err != nil {
		return err
	}
	return tx.Commit()
}

// PortalSetAvatar 玩家在服务中心改头像：扣 5000 金分（50 金币），写头像 URL 与流水。
func (d *DB) PortalSetAvatar(ctx context.Context, uid int64, avatar string, now int64) error {
	if uid <= 0 || avatar == "" || len(avatar) > 512 {
		return fmt.Errorf("头像地址无效")
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var gold, membership, membershipExp int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(gold,0),COALESCE(membership,0),COALESCE(membership_exp,0) FROM profile WHERE uid=?`, uid).
		Scan(&gold, &membership, &membershipExp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("角色不存在")
		}
		return err
	}
	price := portalCustomizePrice(MembershipAt(membership, membershipExp, now).Active)
	if gold < price {
		return fmt.Errorf("金币不足，需要 %d 金分（%d 金币）", price, price/100)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE profile SET avatar=?,gold=gold-?,updated_ts=? WHERE uid=?`,
		avatar, price, float64(now), uid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO txn(uid,ts,kind,item_id,gold_delta,note) VALUES(?,?,'portal_avatar','',?,'portal_customize')`,
		uid, now, -price); err != nil {
		return err
	}
	return tx.Commit()
}

// PortalCustomizeStatus 返回当前余额与单价，供前端展示。
func (d *DB) PortalCustomizeStatus(ctx context.Context, uid int64) (goldMinor, priceMinor int64, err error) {
	var membership, membershipExp int64
	err = d.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(gold,0),COALESCE(membership,0),COALESCE(membership_exp,0) FROM profile WHERE uid=?`, uid).
		Scan(&goldMinor, &membership, &membershipExp)
	priceMinor = portalCustomizePrice(MembershipAt(membership, membershipExp, time.Now().Unix()).Active)
	return
}


// 银币兑换金币费率（银分/100金分）。会员享受「倍率 +0.2」，即同样银币多换 20%。
const (
	// PortalExchangeSilverPer100Gold 非会员：2000000 银分 → 100 金分。
	PortalExchangeSilverPer100Gold int64 = 2000000
	// PortalExchangeSilverPer100GoldPremium 会员：1800000 银分 → 100 金分（倍率 1.2）。
	PortalExchangeSilverPer100GoldPremium int64 = 1800000
)

// PortalExchangeResult 兑换结果。
type PortalExchangeResult struct {
	GoldGained   int64 `json:"goldGained"`   // 本次获得金分
	SilverSpent  int64 `json:"silverSpent"`  // 本次消耗银分
	SilverMinor  int64 `json:"silverMinor"`  // 兑换后银分余额
	GoldMinor    int64 `json:"goldMinor"`    // 兑换后金分余额
	Premium      bool  `json:"premium"`      // 是否按会员费率
}

// PortalSilverToGold 玩家在个人中心用银币兑换金币。
// units 是「100 金分」的份数，每份消耗 2000000 银分（会员 1800000）。
// 事务内加写锁防止并发双花，并写 txn 流水（一负一正）。
func (d *DB) PortalSilverToGold(ctx context.Context, uid, units int64, now int64) (*PortalExchangeResult, error) {
	if uid <= 0 || units <= 0 || units > 10000 {
		return nil, fmt.Errorf("兑换数量需为 1~10000")
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// 先加写锁，避免并发读旧余额。
	if _, err := tx.ExecContext(ctx, `UPDATE profile SET uid=uid WHERE uid=?`, uid); err != nil {
		return nil, err
	}
	var silver, gold, membership, membershipExp int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(silver,0),COALESCE(gold,0),COALESCE(membership,0),COALESCE(membership_exp,0) FROM profile WHERE uid=?`,
		uid).Scan(&silver, &gold, &membership, &membershipExp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("角色不存在")
		}
		return nil, err
	}
	premium := MembershipAt(membership, membershipExp, now).Active
	rate := PortalExchangeSilverPer100Gold
	if premium {
		rate = PortalExchangeSilverPer100GoldPremium
	}
	silverSpent := rate * units
	goldGained := 100 * units
	if silver < silverSpent {
		if premium {
			return nil, fmt.Errorf("银币不足，本次需要 %d 银币（会员费率 %d 银币/100金币）", silverSpent/100, rate/100)
		}
		return nil, fmt.Errorf("银币不足，本次需要 %d 银币（%d 银币/100金币）", silverSpent/100, rate/100)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE profile SET silver=silver-?,gold=gold+?,updated_ts=? WHERE uid=?`,
		silverSpent, goldGained, float64(now), uid); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO txn(uid,ts,kind,item_id,silver_delta,note) VALUES(?,?,'portal_exchange_silver','',?,'portal_silver_to_gold')`,
		uid, now, -silverSpent); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO txn(uid,ts,kind,item_id,gold_delta,note) VALUES(?,?,'portal_exchange_gold','',?,'portal_silver_to_gold')`,
		uid, now, goldGained); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PortalExchangeResult{
		GoldGained: goldGained, SilverSpent: silverSpent,
		SilverMinor: silver - silverSpent, GoldMinor: gold + goldGained, Premium: premium,
	}, nil
}
