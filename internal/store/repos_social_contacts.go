package store

import (
	"context"
)

// AllProfileUIDs 返回所有角色 uid（含离线），供 24/4 联系人列表下发。
// 官服 24/4 返回频道成员 uid 列表；单机服以全部角色作为可见联系人，
// 这样玩家之间即使不同时在线也能在列表里看到彼此并打开资料卡。
func (d *DB) AllProfileUIDs(ctx context.Context) ([]int64, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT uid FROM profile WHERE uid>0 ORDER BY uid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}
