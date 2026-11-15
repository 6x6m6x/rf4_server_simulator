package store

import "testing"

// TestMigrateRetiresLegacyBsSalmonRow 复刻本服用户的升级路径：上一版本地合并版
// 把 00061 给了隆头鳟三色标，而新主线 00061 是 rf4records_catch。
//
// 本版不再自带 00061_add_bs_salmon（主线已用 00062 提供同一份数据），于是旧库里
// 那条登记属于「文件已不存在」的 stale 行，由 retireStaleMigrationRows 退役
// （改成 retired/ 前缀 + 9_000_000 段编号）。退役后 61 让位，主线迁移才能正常
// 登记；否则 schema_migrations.version 主键冲突会让服务起不来。
func TestMigrateRetiresLegacyBsSalmonRow(t *testing.T) {
	db := openCatchHistoryDBAtVersion(t, 60)
	// 上一版真实状态：00061 登记 + 数据行都在。
	if _, err := db.SQL.Exec(
		`INSERT INTO schema_migrations VALUES(61,'migrations/00061_add_bs_salmon_grade_line.sql',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(
		`INSERT INTO fish_grade_lines(fish_key, qualified_g, trophy_g, blue_trophy_g, source, checked_at)
		 VALUES ('bs_salmon', 600, 14000, 20000, 'manual/rf4-stat+l_salmon参照', '2026-11-15')`); err != nil {
		t.Fatal(err)
	}

	if err := db.Migrate(); err != nil {
		t.Fatalf("旧 00061 登记行挡住了主线迁移: %v", err)
	}

	// 主线 00061 与 00062 都应登记成功。
	assertCount(t, db,
		`SELECT COUNT(*) FROM schema_migrations WHERE version=61 AND name='migrations/00061_rf4records_catch.sql'`, 1)
	assertCount(t, db,
		`SELECT COUNT(*) FROM schema_migrations WHERE version=62 AND name='migrations/00062_add_bs_salmon_grade_line.sql'`, 1)
	// 旧登记行应被退役，且不再占用原编号与原名。
	assertCount(t, db,
		`SELECT COUNT(*) FROM schema_migrations WHERE name='migrations/00061_add_bs_salmon_grade_line.sql'`, 0)
	assertCount(t, db,
		`SELECT COUNT(*) FROM schema_migrations WHERE version=9000061
		   AND name='retired/migrations/00061_add_bs_salmon_grade_line.sql'`, 1)
	// 三色标数据不能重复也不能丢失（00062 是 INSERT OR IGNORE）。
	assertCount(t, db, `SELECT COUNT(*) FROM fish_grade_lines WHERE fish_key='bs_salmon'`, 1)
	assertCount(t, db,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='rf4records_catch'`, 1)

	if err := db.Migrate(); err != nil {
		t.Fatalf("二次迁移应幂等: %v", err)
	}
	assertCount(t, db, `SELECT COUNT(*) FROM fish_grade_lines WHERE fish_key='bs_salmon'`, 1)
}

// TestMigrateAppliesBsSalmonOnFreshMainDB 覆盖全新主线库：没有旧登记行时，
// 00062 正常执行并补上三色标数据。
func TestMigrateAppliesBsSalmonOnFreshMainDB(t *testing.T) {
	db := openCatchHistoryDBAtVersion(t, 60)
	if err := db.Migrate(); err != nil {
		t.Fatalf("主线库应能直接升到 00062: %v", err)
	}
	assertCount(t, db,
		`SELECT COUNT(*) FROM schema_migrations WHERE version=61 AND name='migrations/00061_rf4records_catch.sql'`, 1)
	assertCount(t, db,
		`SELECT COUNT(*) FROM schema_migrations WHERE version=62 AND name='migrations/00062_add_bs_salmon_grade_line.sql'`, 1)
	assertCount(t, db, `SELECT COUNT(*) FROM fish_grade_lines WHERE fish_key='bs_salmon'`, 1)
}
