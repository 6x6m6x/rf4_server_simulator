package store

import (
	"context"
	"database/sql"
	"strings"
)

// CafeBand 一个金币梯度档位的区间（金币分，100 分 = 1 金）。
type CafeBand struct {
	Key     string `json:"key"`
	LoCents int64  `json:"lo_cents"`
	HiCents int64  `json:"hi_cents"`
	Label   string `json:"label"`
}

// CafeFish 悬赏鱼白名单一行。Category 为 rare（稀有鱼）或 giant（巨物）。
type CafeFish struct {
	FishKey  string `json:"fish_key"`
	Category string `json:"category"`
	NameZH   string `json:"name_zh"`
	Note     string `json:"note"`
}

// CafeSubsidy 巨物重量级补贴区间。
type CafeSubsidy struct {
	Grp       string `json:"grp"`
	MinBlueG  int64  `json:"min_blue_g"`
	LoCents   int64  `json:"lo_cents"`
	HiCents   int64  `json:"hi_cents"`
	Label     string `json:"label"`
}

// CafeOrderQuantity 一个「重量档 × 稀缺」组合的交单数量。
// QKey 取值 common_normal/common_qualified/rare_normal/rare_qualified，仅用于 7 个普通/达标位。
type CafeOrderQuantity struct {
	QKey     string `json:"qkey"`
	Quantity int64  `json:"quantity"`
	Label    string `json:"label"`
}

// CafeSlotPeriod 一类槽位的轮换周期（秒），min=max 表示固定周期。
// Kind 取值 trophy/longterm/normal。
type CafeSlotPeriod struct {
	Kind       string `json:"kind"`
	MinSeconds int64  `json:"min_seconds"`
	MaxSeconds int64  `json:"max_seconds"`
	Label      string `json:"label"`
}

// CafeSlotMix 一类槽位的数量占比；三类总和固定为 10。
// Kind 取值 trophy/longterm/normal。
type CafeSlotMix struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
	Label string `json:"label"`
}

// CafeBlacklist 一条按水域的悬赏黑名单：该 level 的候选池不再出现该鱼种。
type CafeBlacklist struct {
	Level   string `json:"level"`
	FishKey string `json:"fish_key"`
	NameZH  string `json:"name_zh"`
}

// CafeBountyConfig 后台可维护的全部悬赏配置。
// Fish 仍保留字段以兼容既有读取，但白名单已固化到 gameserver 静态文件，后台不再维护。
type CafeBountyConfig struct {
	Bands      []CafeBand          `json:"bands"`
	Fish       []CafeFish          `json:"fish"`
	Subsidies  []CafeSubsidy       `json:"subsidies"`
	Quantities []CafeOrderQuantity `json:"quantities"`
	Periods    []CafeSlotPeriod    `json:"periods"`
	SlotMix    []CafeSlotMix       `json:"slot_mix"`
	Blacklist  []CafeBlacklist     `json:"blacklist"`
}

// CafeFishSearchResult 后台按中文名/鱼种 key 模糊搜索鱼种的返回项。
type CafeFishSearchResult struct {
	FishKey string `json:"fish_key"`
	NameZH  string `json:"name_zh"`
}

// CafeBountyPoolRow 预生成候选池的一条候选。
type CafeBountyPoolRow struct {
	Level         string  `json:"level"`
	Slot          int     `json:"slot"`
	Variant       int     `json:"variant"`
	Species       string  `json:"species"`
	NameZH        string  `json:"name_zh"`
	WeightG       int64   `json:"weight_g"`
	Grade         int64   `json:"grade"`
	Rarity        float64 `json:"rarity"`
	IsRare        bool    `json:"is_rare"`
	IsGiant       bool    `json:"is_giant"`
	GoldCents     int64   `json:"gold_cents"`
	SubsidyCents  int64   `json:"subsidy_cents"`
	PeriodSeconds int64   `json:"period_seconds"`
	PhaseSeconds  int64   `json:"phase_seconds"`
	Quantity      int64   `json:"quantity"`
}

func (d *DB) CafeBountyBands(ctx context.Context) ([]CafeBand, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT band_key,lo_cents,hi_cents,label FROM cafe_bounty_band ORDER BY band_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CafeBand{}
	for rows.Next() {
		var b CafeBand
		if err := rows.Scan(&b.Key, &b.LoCents, &b.HiCents, &b.Label); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (d *DB) CafeBountyFish(ctx context.Context) ([]CafeFish, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT fish_key,category,name_zh,note FROM cafe_bounty_fish ORDER BY category,fish_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CafeFish{}
	for rows.Next() {
		var f CafeFish
		if err := rows.Scan(&f.FishKey, &f.Category, &f.NameZH, &f.Note); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (d *DB) CafeGiantSubsidies(ctx context.Context) ([]CafeSubsidy, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT grp,min_blue_g,lo_cents,hi_cents,label FROM cafe_giant_subsidy ORDER BY min_blue_g DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CafeSubsidy{}
	for rows.Next() {
		var s CafeSubsidy
		if err := rows.Scan(&s.Grp, &s.MinBlueG, &s.LoCents, &s.HiCents, &s.Label); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) CafeOrderQuantities(ctx context.Context) ([]CafeOrderQuantity, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT qkey,quantity,label FROM cafe_order_quantity ORDER BY qkey`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CafeOrderQuantity{}
	for rows.Next() {
		var q CafeOrderQuantity
		if err := rows.Scan(&q.QKey, &q.Quantity, &q.Label); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (d *DB) CafeSlotPeriods(ctx context.Context) ([]CafeSlotPeriod, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT kind,min_seconds,max_seconds,label FROM cafe_slot_period ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CafeSlotPeriod{}
	for rows.Next() {
		var p CafeSlotPeriod
		if err := rows.Scan(&p.Kind, &p.MinSeconds, &p.MaxSeconds, &p.Label); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *DB) CafeBountyConfig(ctx context.Context) (CafeBountyConfig, error) {
	var cfg CafeBountyConfig
	var err error
	if cfg.Bands, err = d.CafeBountyBands(ctx); err != nil {
		return cfg, err
	}
	if cfg.Fish, err = d.CafeBountyFish(ctx); err != nil {
		return cfg, err
	}
	if cfg.Subsidies, err = d.CafeGiantSubsidies(ctx); err != nil {
		return cfg, err
	}
	if cfg.Quantities, err = d.CafeOrderQuantities(ctx); err != nil {
		return cfg, err
	}
	if cfg.Periods, err = d.CafeSlotPeriods(ctx); err != nil {
		return cfg, err
	}
	if cfg.SlotMix, err = d.CafeSlotMixes(ctx); err != nil {
		return cfg, err
	}
	if cfg.Blacklist, err = d.CafeBlacklistAll(ctx); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// CafeBountyBandSet 更新一个档位区间；范围非法返回 ErrCafeOrderInvalid。
func (d *DB) CafeBountyBandSet(ctx context.Context, key string, lo, hi int64) error {
	if key == "" || lo < 0 || hi < lo {
		return ErrCafeOrderInvalid
	}
	_, err := d.SQL.ExecContext(ctx, `UPDATE cafe_bounty_band SET lo_cents=?,hi_cents=? WHERE band_key=?`, lo, hi, key)
	return err
}

// CafeBountyFishAdd 添加或替换一条白名单（upsert）。
func (d *DB) CafeBountyFishAdd(ctx context.Context, f CafeFish) error {
	if f.FishKey == "" || (f.Category != "rare" && f.Category != "giant") {
		return ErrCafeOrderInvalid
	}
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO cafe_bounty_fish(fish_key,category,name_zh,note)
		VALUES(?,?,?,?) ON CONFLICT(fish_key) DO UPDATE SET
		category=excluded.category,name_zh=excluded.name_zh,note=excluded.note`,
		f.FishKey, f.Category, f.NameZH, f.Note)
	return err
}

func (d *DB) CafeBountyFishRemove(ctx context.Context, fishKey string) error {
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM cafe_bounty_fish WHERE fish_key=?`, fishKey)
	return err
}

// CafeGiantSubsidySet 更新一个重量级补贴区间。
func (d *DB) CafeGiantSubsidySet(ctx context.Context, grp string, minBlueG, lo, hi int64) error {
	if grp == "" || minBlueG < 0 || lo < 0 || hi < lo {
		return ErrCafeOrderInvalid
	}
	_, err := d.SQL.ExecContext(ctx, `UPDATE cafe_giant_subsidy SET min_blue_g=?,lo_cents=?,hi_cents=? WHERE grp=?`, minBlueG, lo, hi, grp)
	return err
}

// CafeOrderQuantitySet 更新一个交单数量；qkey 必须合法，数量 1~1000。
func (d *DB) CafeOrderQuantitySet(ctx context.Context, qkey string, quantity int64) error {
	if qkey == "" || quantity < 1 || quantity > 1000 {
		return ErrCafeOrderInvalid
	}
	_, err := d.SQL.ExecContext(ctx, `UPDATE cafe_order_quantity SET quantity=? WHERE qkey=?`, quantity, qkey)
	return err
}

// CafeSlotPeriodSet 更新一类槽位的轮换周期（秒）；kind 必须合法。
func (d *DB) CafeSlotPeriodSet(ctx context.Context, kind string, minSeconds, maxSeconds int64) error {
	if kind == "" || minSeconds <= 0 || maxSeconds < minSeconds {
		return ErrCafeOrderInvalid
	}
	_, err := d.SQL.ExecContext(ctx, `UPDATE cafe_slot_period SET min_seconds=?,max_seconds=? WHERE kind=?`, minSeconds, maxSeconds, kind)
	return err
}

// CafeSlotMixes 读取三类槽位数量。
func (d *DB) CafeSlotMixes(ctx context.Context) ([]CafeSlotMix, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT kind,count,label FROM cafe_slot_mix ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CafeSlotMix{}
	for rows.Next() {
		var m CafeSlotMix
		if err := rows.Scan(&m.Kind, &m.Count, &m.Label); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CafeSlotMixSetAll 一次写入三类槽位数量；三类总和必须为 10，否则返回 ErrCafeOrderInvalid。
// 用事务保证要么全部生效，要么全部回滚。
func (d *DB) CafeSlotMixSetAll(ctx context.Context, counts map[string]int) error {
	kinds := []string{"trophy", "longterm", "normal"}
	total := 0
	for _, k := range kinds {
		c, ok := counts[k]
		if !ok || c < 0 || c > 10 {
			return ErrCafeOrderInvalid
		}
		total += c
	}
	if total != 10 {
		return ErrCafeOrderInvalid
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, k := range kinds {
		if _, err := tx.ExecContext(ctx, `UPDATE cafe_slot_mix SET count=? WHERE kind=?`, counts[k], k); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CafeBlacklistAll 读取全部黑名单（按 level、fish_key 排序）。
func (d *DB) CafeBlacklistAll(ctx context.Context) ([]CafeBlacklist, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT level,fish_key,name_zh FROM cafe_bounty_blacklist ORDER BY level,fish_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CafeBlacklist{}
	for rows.Next() {
		var b CafeBlacklist
		if err := rows.Scan(&b.Level, &b.FishKey, &b.NameZH); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CafeBlacklistByLevel 读取一个水域的黑名单。
func (d *DB) CafeBlacklistByLevel(ctx context.Context, level string) ([]CafeBlacklist, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT level,fish_key,name_zh FROM cafe_bounty_blacklist WHERE level=? ORDER BY fish_key`, level)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CafeBlacklist{}
	for rows.Next() {
		var b CafeBlacklist
		if err := rows.Scan(&b.Level, &b.FishKey, &b.NameZH); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CafeBlacklistAdd 加入一条按水域的黑名单（upsert）。
func (d *DB) CafeBlacklistAdd(ctx context.Context, b CafeBlacklist) error {
	if b.Level == "" || b.FishKey == "" {
		return ErrCafeOrderInvalid
	}
	_, err := d.SQL.ExecContext(ctx, `INSERT INTO cafe_bounty_blacklist(level,fish_key,name_zh)
		VALUES(?,?,?) ON CONFLICT(level,fish_key) DO UPDATE SET name_zh=excluded.name_zh`,
		b.Level, b.FishKey, b.NameZH)
	return err
}

// CafeBlacklistRemove 删除一条按水域的黑名单。
func (d *DB) CafeBlacklistRemove(ctx context.Context, level, fishKey string) error {
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM cafe_bounty_blacklist WHERE level=? AND fish_key=?`, level, fishKey)
	return err
}

// CafeFishSearch 按中文名或 fish_key 模糊搜索可钓鱼种，供后台黑名单添加窗口用。
// 数据源取 loc_fish + item_catalog 中文名；limit 防止一次返回过多。
func (d *DB) CafeFishSearch(ctx context.Context, q string, limit int) ([]CafeFishSearchResult, error) {
	q = strings.TrimSpace(q)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	like := "%" + q + "%"
	rows, err := d.SQL.QueryContext(ctx, `SELECT f.fish_key,
		COALESCE(NULLIF(c.name_zh,''),NULLIF(f.name_zh,''),f.fish_key) AS name_zh
		FROM (SELECT fish_key, MAX(name_zh) AS name_zh FROM loc_fish GROUP BY fish_key) f
		LEFT JOIN item_catalog c ON c.item_id=f.fish_key
		WHERE f.fish_key LIKE ? OR COALESCE(NULLIF(c.name_zh,''),NULLIF(f.name_zh,''),'') LIKE ?
		ORDER BY name_zh LIMIT ?`, like, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CafeFishSearchResult{}
	for rows.Next() {
		var r CafeFishSearchResult
		if err := rows.Scan(&r.FishKey, &r.NameZH); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CafeBountyPoolReplace 原子替换指定水域的候选池；emptyLevel 表示清空全部。
func (d *DB) CafeBountyPoolReplace(ctx context.Context, level string, rows []CafeBountyPoolRow) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM cafe_bounty_pool WHERE level=?`, level); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO cafe_bounty_pool
		(level,slot,variant,species,name_zh,weight_g,grade,rarity,is_rare,is_giant,gold_cents,subsidy_cents,period_seconds,phase_seconds,quantity)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		qty := r.Quantity
		if qty <= 0 {
			qty = 1
		}
		if _, err := stmt.ExecContext(ctx, r.Level, r.Slot, r.Variant, r.Species, r.NameZH,
			r.WeightG, r.Grade, r.Rarity, boolInt(r.IsRare), boolInt(r.IsGiant),
			r.GoldCents, r.SubsidyCents, r.PeriodSeconds, r.PhaseSeconds, qty); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CafeBountyPoolByLevel 读取指定水域候选池，按 slot、variant 排序。
func (d *DB) CafeBountyPoolByLevel(ctx context.Context, level string) ([]CafeBountyPoolRow, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT level,slot,variant,species,name_zh,weight_g,grade,rarity,is_rare,is_giant,gold_cents,subsidy_cents,period_seconds,phase_seconds,quantity
		FROM cafe_bounty_pool WHERE level=? ORDER BY slot,variant`, level)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCafePool(rows)
}

func (d *DB) CafeBountyPoolAll(ctx context.Context) ([]CafeBountyPoolRow, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT level,slot,variant,species,name_zh,weight_g,grade,rarity,is_rare,is_giant,gold_cents,subsidy_cents,period_seconds,phase_seconds,quantity
		FROM cafe_bounty_pool ORDER BY level,slot,variant`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCafePool(rows)
}

func scanCafePool(rows *sql.Rows) ([]CafeBountyPoolRow, error) {
	out := []CafeBountyPoolRow{}
	for rows.Next() {
		var r CafeBountyPoolRow
		var isRare, isGiant int
		if err := rows.Scan(&r.Level, &r.Slot, &r.Variant, &r.Species, &r.NameZH, &r.WeightG,
			&r.Grade, &r.Rarity, &isRare, &isGiant, &r.GoldCents, &r.SubsidyCents,
			&r.PeriodSeconds, &r.PhaseSeconds, &r.Quantity); err != nil {
			return nil, err
		}
		r.IsRare, r.IsGiant = isRare != 0, isGiant != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// CafePoolLevels 返回已生成候选池的全部水域。
func (d *DB) CafePoolLevels(ctx context.Context) ([]string, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT DISTINCT level FROM cafe_bounty_pool ORDER BY level`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CafeBountyLevels 返回所有可钓鱼域的 level key（loc_alias.alias 且 loc_fish 中有鱼）。
// 候选池唯一 level 来源，撇开 cafe_reward_templates.json。
func (d *DB) CafeBountyLevels(ctx context.Context) ([]string, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT DISTINCT a.alias FROM loc_alias a
		WHERE a.alias LIKE 'level_%' AND EXISTS(SELECT 1 FROM loc_fish f WHERE f.loc=a.loc)
		ORDER BY a.alias`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CafeLocFish 一条可钓鱼种及其权重、中文名、档位线。
type CafeLocFish struct {
	Loc        string
	FishKey    string
	NameZH     string
	Catches    int64
	QualifiedG int64
	TrophyG    int64
	BlueG      int64
}

// CafeLocFishes 取某水域全部可钓鱼种（左连接档位线与中文名，缺失线以 0 表示）。
func (d *DB) CafeLocFishes(ctx context.Context, loc string) ([]CafeLocFish, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT f.loc,f.fish_key,COALESCE(NULLIF(c.name_zh,''),NULLIF(f.name_zh,''),f.fish_key),
		f.catches,COALESCE(g.qualified_g,0),COALESCE(g.trophy_g,0),COALESCE(g.blue_trophy_g,0)
		FROM loc_fish f
		LEFT JOIN item_catalog c ON c.item_id=f.fish_key
		LEFT JOIN fish_grade_lines g ON g.fish_key=f.fish_key
		WHERE f.loc=? ORDER BY f.fish_key`, loc)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CafeLocFish{}
	for rows.Next() {
		var f CafeLocFish
		if err := rows.Scan(&f.Loc, &f.FishKey, &f.NameZH, &f.Catches, &f.QualifiedG, &f.TrophyG, &f.BlueG); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
