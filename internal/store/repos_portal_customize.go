package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PortalCustomizePriceGold 服务中心改名/改头像单价：20 金币（金币以分计，2000）。
const PortalCustomizePriceGold = 2000

// PortalRename 玩家在服务中心改名：扣 20 金币，写新昵称与流水。
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
	var gold int64
	if err := tx.QueryRowContext(ctx, `SELECT gold FROM profile WHERE uid=?`, uid).Scan(&gold); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("角色不存在")
		}
		return err
	}
	if gold < PortalCustomizePriceGold {
		return fmt.Errorf("金币不足，需要 20 金币")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE profile SET name=?,gold=gold-?,updated_ts=? WHERE uid=?`,
		name, PortalCustomizePriceGold, float64(now), uid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO txn(uid,ts,kind,item_id,gold_delta,note) VALUES(?,?,'portal_rename','',?,'portal_customize')`,
		uid, now, -PortalCustomizePriceGold); err != nil {
		return err
	}
	return tx.Commit()
}

// PortalSetAvatar 玩家在服务中心改头像：扣 20 金币，写头像 URL 与流水。
func (d *DB) PortalSetAvatar(ctx context.Context, uid int64, avatar string, now int64) error {
	if uid <= 0 || avatar == "" || len(avatar) > 512 {
		return fmt.Errorf("头像地址无效")
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var gold int64
	if err := tx.QueryRowContext(ctx, `SELECT gold FROM profile WHERE uid=?`, uid).Scan(&gold); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("角色不存在")
		}
		return err
	}
	if gold < PortalCustomizePriceGold {
		return fmt.Errorf("金币不足，需要 20 金币")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE profile SET avatar=?,gold=gold-?,updated_ts=? WHERE uid=?`,
		avatar, PortalCustomizePriceGold, float64(now), uid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO txn(uid,ts,kind,item_id,gold_delta,note) VALUES(?,?,'portal_avatar','',?,'portal_customize')`,
		uid, now, -PortalCustomizePriceGold); err != nil {
		return err
	}
	return tx.Commit()
}

// PortalCustomizeStatus 返回当前余额与单价，供前端展示。
func (d *DB) PortalCustomizeStatus(ctx context.Context, uid int64) (goldMinor, priceMinor int64, err error) {
	err = d.SQL.QueryRowContext(ctx, `SELECT gold FROM profile WHERE uid=?`, uid).Scan(&goldMinor)
	priceMinor = PortalCustomizePriceGold
	return
}

