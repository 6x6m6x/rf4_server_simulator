package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// 欢乐时光：会员专享，每个自然日 1 小时（Asia/Shanghai 零点重置）。
// 普通玩家没有资格（Qualified=false），不享受任何加成。
// 会员资格：永久会员，或限时会员且未过期；到期立即失去资格。
// 额度只在同一在线会话"正在钓鱼"时累计；离线、换会话、进程重启都不补扣。

const (
	HappyHourNone      = int64(0)
	HappyHourLimited   = int64(1)
	HappyHourUnlimited = int64(2)
)

// HappyHourMemberMillis 会员每日欢乐时光额度：1 小时。
const HappyHourMemberMillis = int64(60 * 60 * 1000)

// HappyHourUnlimitedSeconds 历史哨兵，仅供旧后台展示兼容，不再表示无限时长。
const HappyHourUnlimitedSeconds = int64(9999 * 60)

var happyHourZone = time.FixedZone("Asia/Shanghai", 8*60*60)

type HappyHourEntitlement struct {
	Qualified        bool  `json:"qualified"`         // 是否会员（有资格）
	Member           bool  `json:"member"`            // 同 Qualified，兼容补丁字段
	Unlimited        bool  `json:"unlimited"`         // 兼容字段，恒 false
	ExpiresAt        int64 `json:"expires_at"`        // 会员到期时间，仅供展示
	QuotaMillis      int64 `json:"quota_millis"`      // 当日额度上限：会员 3600000，普通 0
	RemainingSeconds int64 `json:"remaining_seconds"` // 兼容展示用（剩余秒）
}

// HappyHourEntitled 计算会员资格与当日额度。普通玩家额度为 0（无欢乐时光）。
func HappyHourEntitled(kind, expiresAt, now int64) HappyHourEntitlement {
	member := kind == HappyHourUnlimited || (kind == HappyHourLimited && expiresAt > now)
	if !member {
		return HappyHourEntitlement{ExpiresAt: expiresAt}
	}
	return HappyHourEntitlement{
		Qualified: true, Member: true, ExpiresAt: expiresAt,
		QuotaMillis: HappyHourMemberMillis, RemainingSeconds: HappyHourMemberMillis / 1000,
	}
}

type HappyHourStatus struct {
	Enabled          bool    `json:"enabled"`
	Qualified        bool    `json:"qualified"`
	Member           bool    `json:"member"`
	Unlimited        bool    `json:"unlimited"`
	ExpiresAt        int64   `json:"expires_at"`
	Started          bool    `json:"started"`
	Active           bool    `json:"active"`
	UsedMillis       int64   `json:"used_millis"`
	QuotaMillis      int64   `json:"quota_millis"`
	RemainingSeconds int64   `json:"remaining_seconds"`
	XPBonus          float64 `json:"xp_bonus"`
	BiteBonus        float64 `json:"bite_bonus"`
	ProficiencyBonus float64 `json:"proficiency_bonus"`
}

func happyHourDay(now time.Time) string { return now.In(happyHourZone).Format("2006-01-02") }

func happyHourStatus(enabled bool, ent HappyHourEntitlement, started, active bool, used int64) HappyHourStatus {
	remaining := int64(0)
	if ent.Qualified {
		remaining = (ent.QuotaMillis - used) / 1000
		if remaining < 0 {
			remaining = 0
		}
	}
	r := HappyHourStatus{
		Enabled: enabled, Qualified: ent.Qualified, Member: ent.Member,
		Unlimited: false, ExpiresAt: ent.ExpiresAt, Started: started,
		Active: enabled && ent.Qualified && active,
		UsedMillis: used, QuotaMillis: ent.QuotaMillis, RemainingSeconds: remaining,
	}
	if r.Active {
		r.XPBonus, r.BiteBonus, r.ProficiencyBonus = .5, .1, .25
	}
	return r
}

func happyHourEntitlementTx(ctx context.Context, tx *sql.Tx, uid, now int64) (HappyHourEntitlement, error) {
	var kind, expires int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(membership,0),COALESCE(membership_exp,0) FROM profile WHERE uid=?`, uid).
		Scan(&kind, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return HappyHourEntitlement{}, nil
	}
	if err != nil {
		return HappyHourEntitlement{}, err
	}
	return HappyHourEntitled(kind, expires, now), nil
}

func (d *DB) happyHourEntitlement(ctx context.Context, uid, now int64) (HappyHourEntitlement, error) {
	var kind, expires int64
	err := d.SQL.QueryRowContext(ctx, `SELECT COALESCE(membership,0),COALESCE(membership_exp,0) FROM profile WHERE uid=?`, uid).
		Scan(&kind, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return HappyHourEntitlement{}, nil
	}
	if err != nil {
		return HappyHourEntitlement{}, err
	}
	return HappyHourEntitled(kind, expires, now), nil
}

// AdvanceHappyHour 会员每日额度：按 Asia/Shanghai 自然日重置，只在同一在线会话钓鱼时累计。
func (d *DB) AdvanceHappyHour(ctx context.Context, uid int64, owner string, now time.Time, fishing, takeover bool, maxGap time.Duration) (HappyHourStatus, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return HappyHourStatus{}, err
	}
	defer tx.Rollback()
	// 写锁：多个连接不能同时消费同一账户的时长。
	if _, err := tx.ExecContext(ctx, `UPDATE happy_hour_state SET active=active WHERE uid=?`, uid); err != nil {
		return HappyHourStatus{}, err
	}
	enabled := true
	var flag string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM tunables WHERE key='happy_hour_enabled'`).Scan(&flag); err == nil {
		enabled = flag == "1"
	} else if !errors.Is(err, sql.ErrNoRows) {
		return HappyHourStatus{}, err
	}
	ent, err := happyHourEntitlementTx(ctx, tx, uid, now.Unix())
	if err != nil {
		return HappyHourStatus{}, err
	}
	var day, previousOwner string
	var used, last int64
	var previousActive bool
	err = tx.QueryRowContext(ctx, `SELECT local_day,used_millis,last_tick,session_key,active FROM happy_hour_state WHERE uid=?`, uid).
		Scan(&day, &used, &last, &previousOwner, &previousActive)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return HappyHourStatus{}, err
	}
	millis, currentDay := now.UnixMilli(), happyHourDay(now)
	// 另一在线会话仍活跃时，新会话不能顶掉它。
	if exists && previousOwner != owner && (!takeover || previousActive && millis-last < max(30000, maxGap.Milliseconds())) {
		return happyHourStatus(enabled, ent, true, false, used), nil
	}
	if !exists || day != currentDay {
		used = 0
		if exists && previousOwner == owner && previousActive {
			local := now.In(happyHourZone)
			midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, happyHourZone).UnixMilli()
			last = max(last, midnight)
		} else {
			last = millis
		}
	}
	if exists && previousOwner == owner && previousActive {
		elapsed := max(int64(0), millis-last)
		if maxGap > 0 {
			elapsed = min(elapsed, maxGap.Milliseconds())
		}
		used = min(ent.QuotaMillis, used+elapsed)
	}
	// 只有会员（QuotaMillis>0）且额度未用尽才激活。
	active := fishing && enabled && ent.Qualified && used < ent.QuotaMillis
	if !exists && !active {
		return happyHourStatus(enabled, ent, false, false, 0), nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO happy_hour_state(uid,local_day,used_millis,last_tick,session_key,active)
		VALUES(?,?,?,?,?,?) ON CONFLICT(uid) DO UPDATE SET local_day=excluded.local_day,
		used_millis=excluded.used_millis,last_tick=excluded.last_tick,session_key=excluded.session_key,active=excluded.active`,
		uid, currentDay, used, millis, owner, active); err != nil {
		return HappyHourStatus{}, err
	}
	if err := tx.Commit(); err != nil {
		return HappyHourStatus{}, err
	}
	return happyHourStatus(enabled, ent, true, active, used), nil
}

func (d *DB) GetHappyHourStatus(ctx context.Context, uid int64, now time.Time) (HappyHourStatus, error) {
	enabled := true
	if flag, found, err := d.TunableGet(ctx, "happy_hour_enabled"); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return HappyHourStatus{}, err
	} else if found {
		enabled = flag == "1"
	}
	ent, err := d.happyHourEntitlement(ctx, uid, now.Unix())
	if err != nil {
		return HappyHourStatus{}, err
	}
	var day string
	var used, last int64
	var active bool
	err = d.SQL.QueryRowContext(ctx, `SELECT local_day,used_millis,active,last_tick FROM happy_hour_state WHERE uid=?`, uid).
		Scan(&day, &used, &active, &last)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && day != happyHourDay(now)) {
		return happyHourStatus(enabled, ent, false, false, 0), nil
	}
	if err != nil {
		return HappyHourStatus{}, err
	}
	return happyHourStatus(enabled, ent, true, active && now.UnixMilli()-last <= 30000, used), nil
}
