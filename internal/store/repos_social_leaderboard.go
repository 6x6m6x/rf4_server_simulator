package store

import (
	"context"
	"time"
)

// 游戏内记录榜（24/21 绝对记录榜 / 24/24 明星鱼）的数据源。
// 官服实包（2026-11-25）显示榜单是全服共享的「每鱼种 Top5 钓获」：
// 每条记录含重量、地点、饵、日期、经验与玩家信息，全部来自玩家实际钓获。
// 本文件只负责从本地 catch_log 聚合出这些行；JSON 形状由 gameserver 组装。

// LeaderboardRecord 一条上榜钓获（对应官服 records[i].records[j] 的数据源）。
type LeaderboardRecord struct {
	UID      int64
	FishKey  string
	FishName string
	WeightG  int64
	Loc      string // 场景/水域 key
	LocLevel string // level key（优先作为官服 location.id）
	Bait     string // 饵 key，可能是 "a;b" 组合
	TS       float64
	XP       int64
}

// LeaderboardFish 一个鱼种的 Top5 列表。
type LeaderboardFish struct {
	FishKey  string
	FishName string
	Records  []LeaderboardRecord
}

// GlobalLeaderboard 读取全服记录榜。
// id: "absolute"=全部；"weekly"=最近 7 天；level key=该水域。
// perFish 为每个鱼种保留的条数，官服为 5。
func (d *DB) GlobalLeaderboard(ctx context.Context, id string, perFish int) ([]LeaderboardFish, error) {
	if d == nil || d.SQL == nil {
		return nil, nil
	}
	if perFish <= 0 {
		perFish = 5
	}
	where := `WHERE COALESCE(c.fish_key,'')<>'' AND c.uid>0`
	filterArgs := []any{}
	switch {
	case id == "weekly":
		where += ` AND c.ts >= ?`
		filterArgs = append(filterArgs, float64(time.Now().Unix()-7*86400))
	case id != "" && id != "absolute":
		where += ` AND (c.loc_level=? OR c.loc=?)`
		filterArgs = append(filterArgs, id, id)
	}
	// SQL 顺序：WHERE 的占位符在前，rn <= ? 在最后。
	args := append(filterArgs, perFish)
	// 窗口函数按鱼种分组取最重 TopN；同重按时间早、id 小者优先，保证稳定。
	q := `WITH ranked AS (
		SELECT c.uid, c.fish_key, c.name_zh, c.weight_g, c.loc, c.loc_level,
		       c.bait, c.ts, c.xp,
		       ROW_NUMBER() OVER (PARTITION BY c.fish_key
		                          ORDER BY c.weight_g DESC, c.ts ASC, c.id ASC) AS rn
		FROM catch_log c ` + where + `
	)
	SELECT uid, fish_key, name_zh, weight_g, loc, loc_level, bait, ts, xp
	FROM ranked WHERE rn <= ? ORDER BY fish_key, rn`

	rows, err := d.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]LeaderboardFish, 0)
	idx := map[string]int{}
	for rows.Next() {
		var r LeaderboardRecord
		if err := rows.Scan(&r.UID, &r.FishKey, &r.FishName, &r.WeightG,
			&r.Loc, &r.LocLevel, &r.Bait, &r.TS, &r.XP); err != nil {
			return nil, err
		}
		i, ok := idx[r.FishKey]
		if !ok {
			i = len(out)
			idx[r.FishKey] = i
			out = append(out, LeaderboardFish{FishKey: r.FishKey, FishName: r.FishName})
		}
		out[i].Records = append(out[i].Records, r)
	}
	return out, rows.Err()
}

// LeaderboardProfiles 批量读取榜上玩家资料（uid -> Profile）。
func (d *DB) LeaderboardProfiles(ctx context.Context) (map[int64]Profile, error) {
	out := map[int64]Profile{}
	if d == nil || d.SQL == nil {
		return out, nil
	}
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT COALESCE(uid,0), COALESCE(name,''), COALESCE(level,1), COALESCE(region,'CN'),
		        COALESCE(role,0), COALESCE(avatar,'') FROM profile`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Profile
		if err := rows.Scan(&p.UID, &p.Name, &p.Level, &p.Region, &p.Role, &p.Avatar); err != nil {
			continue
		}
		out[p.UID] = p
	}
	return out, rows.Err()
}

// LeaderboardLocNames 读取水域 level key -> 中文名（loc_alias.alias -> loc）。
func (d *DB) LeaderboardLocNames(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	if d == nil || d.SQL == nil {
		return out, nil
	}
	rows, err := d.SQL.QueryContext(ctx, `SELECT alias, loc FROM loc_alias`)
	if err != nil {
		return out, nil // 旧库可能没有该表
	}
	defer rows.Close()
	for rows.Next() {
		var alias, loc string
		if rows.Scan(&alias, &loc) == nil {
			out[alias] = loc
		}
	}
	return out, rows.Err()
}

// LeaderboardBaitNames 读取饵 key -> 中文名（item_catalog.item_id -> name_zh）。
func (d *DB) LeaderboardBaitNames(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	if d == nil || d.SQL == nil {
		return out, nil
	}
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT item_id, COALESCE(name_zh,name,'') FROM item_catalog`)
	if err != nil {
		return out, nil
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) == nil && name != "" {
			out[id] = name
		}
	}
	return out, rows.Err()
}

// NamedFishHistory 一条被命名鱼的全部钓获历史（官服 24/24 history）。
type NamedFishHistory struct {
	ID      int64
	WeightG int64
	LengthM float64
	Bait    string
	TS      float64
}

// NamedFishEntry 被命名鱼（官服 24/24 namedfish 元素）。
type NamedFishEntry struct {
	FishID  string
	Species string
	Name    string
	Owner   string
	Place   string
	History []NamedFishHistory
}

// GlobalNamedFish 读取某个水域的命名鱼榜。place 为空时返回全部。
func (d *DB) GlobalNamedFish(ctx context.Context, place string) ([]NamedFishEntry, error) {
	out := make([]NamedFishEntry, 0)
	if d == nil || d.SQL == nil {
		return out, nil
	}
	q := `SELECT fish_id, species, name, COALESCE(owner,''), COALESCE(place,''),
	             COALESCE(weight_g,0), COALESCE(length_m,0), COALESCE(ts,0) FROM named_fish`
	args := []any{}
	if place != "" {
		q += ` WHERE place=?`
		args = append(args, place)
	}
	q += ` ORDER BY weight_g DESC`
	rows, err := d.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	type base struct {
		weightG int64
		lengthM float64
		ts      float64
	}
	bases := map[string]base{}
	for rows.Next() {
		var e NamedFishEntry
		var b base
		if err := rows.Scan(&e.FishID, &e.Species, &e.Name, &e.Owner, &e.Place,
			&b.weightG, &b.lengthM, &b.ts); err != nil {
			continue
		}
		bases[e.FishID] = b
		e.History = []NamedFishHistory{}
		out = append(out, e)
	}
	rows.Close()
	// 历史来自该命名鱼（按 owner+species+place）的全部钓获，按时间升序。
	for i := range out {
		hrows, err := d.SQL.QueryContext(ctx,
			`SELECT id, weight_g, length_m, bait, ts FROM catch_log
			 WHERE fish_key=? AND loc_level=? ORDER BY ts, id`,
			out[i].Species, out[i].Place)
		if err != nil {
			continue
		}
		for hrows.Next() {
			var h NamedFishHistory
			if hrows.Scan(&h.ID, &h.WeightG, &h.LengthM, &h.Bait, &h.TS) == nil {
				out[i].History = append(out[i].History, h)
			}
		}
		hrows.Close()
		if len(out[i].History) == 0 {
			// 没有历史时用命名鱼自身那条作为单条历史，避免榜单里有空壳。
			b := bases[out[i].FishID]
			out[i].History = append(out[i].History, NamedFishHistory{
				ID: 0, WeightG: b.weightG, LengthM: b.lengthM, Bait: "", TS: b.ts,
			})
		}
	}
	return out, nil
}
