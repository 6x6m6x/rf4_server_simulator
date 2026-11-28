package gameserver

import (
	"context"
	"testing"
)

// TestAdminCompleteAllAchievements：一键点亮全部成就——定义表全量 unlocked+claimed，
// 重复执行幂等（第二次 newly=0）。
func TestAdminCompleteAllAchievements(t *testing.T) {
	srv, db := newProgressSrv(t)
	defer db.Close()
	ctx := context.Background()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil { t.Fatal(err) }
	// 迁移后 achievement_def 已有 130 条；若测试库为空则手工补两条。
	defs, err := db.AchievementDefinitions(ctx)
	if err != nil { t.Fatal(err) }
	if len(defs) == 0 {
		for _, id := range []int64{101, 102} {
			if _, err := db.SQL.ExecContext(ctx, `INSERT INTO achievement_def(id,sort) VALUES(?,?)`, id, id); err != nil { t.Fatal(err) }
		}
	}
	n := srv.completeAllAchievementsForAdmin(uid)
	if n == 0 { t.Fatal("一键完成成就应点亮至少一项") }
	defs, _ = db.AchievementDefinitions(ctx)
	states, err := db.PlayerAchievementStates(ctx, uid)
	if err != nil { t.Fatal(err) }
	for _, id := range defs {
		st := states[id]
		if !st.Unlocked || !st.Claimed { t.Fatalf("成就 %d 未点亮: %+v", id, st) }
	}
	if n2 := srv.completeAllAchievementsForAdmin(uid); n2 != 0 {
		t.Fatalf("重复执行应幂等 newly=0，实得 %d", n2)
	}
}

// TestAdminCompleteAllQualifications：一键完成全部资格——所有非缺口规则 completed_ts>0、
// counted 填满，分支奖杯补发，重复执行幂等。
func TestAdminCompleteAllQualifications(t *testing.T) {
	srv, db := newProgressSrv(t)
	defer db.Close()
	ctx := context.Background()
	const uid = int64(1000002)
	if _, err := db.EnsureProfile(ctx, uid); err != nil { t.Fatal(err) }
	n := srv.completeAllQualificationsForAdmin(uid)
	if n == 0 { t.Fatal("一键完成资格应至少完成一条规则") }
	states, err := db.QualificationStates(ctx, uid)
	if err != nil { t.Fatal(err) }
	want := 0
	for _, r := range qualificationRules {
		if eventGapRuleIDs[r.ID] { continue }
		want++
		st := states[r.ID]
		if st.CompletedTS <= 0 { t.Fatalf("资格规则 %d 未完成", r.ID) }
		if len(st.Counted) < int(qualificationTarget(uint64(r.ID))) {
			t.Fatalf("资格规则 %d 计数不满: %d", r.ID, len(st.Counted))
		}
	}
	if n != want { t.Fatalf("完成规则数 %d，期望 %d", n, want) }
	if n2 := srv.completeAllQualificationsForAdmin(uid); n2 != want {
		t.Fatalf("重复执行应仍为 %d（奖杯不重发），实得 %d", want, n2)
	}
}
