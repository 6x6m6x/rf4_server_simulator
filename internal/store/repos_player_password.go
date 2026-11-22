package store

import (
	"context"
	"database/sql"
	"strings"
)

// GetPasswordSHA1 读取玩家的密码哈希（小写 hex）。
// 空串表示免密；玩家不存在也按空串处理，方便登录侧跳过校验。
func (d *DB) GetPasswordSHA1(ctx context.Context, uid int64) (string, error) {
	var hash string
	err := d.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(password_sha1,'') FROM profile WHERE uid=?`, uid).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return hash, err
}

// FindUIDByName 按游戏内昵称查找 uid。未找到返回 (0, nil)。
func (d *DB) FindUIDByName(ctx context.Context, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, nil
	}
	var uid int64
	err := d.SQL.QueryRowContext(ctx,
		`SELECT uid FROM profile WHERE name=? ORDER BY uid LIMIT 1`, name).Scan(&uid)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return uid, err
}

// SetPassword 写入玩家密码哈希；传空串即清空密码（恢复免密）。
func (d *DB) SetPassword(ctx context.Context, uid int64, sha1Hex string) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE profile SET password_sha1=? WHERE uid=?`, strings.ToLower(strings.TrimSpace(sha1Hex)), uid)
	return err
}
