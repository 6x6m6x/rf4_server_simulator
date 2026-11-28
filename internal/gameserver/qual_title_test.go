package gameserver

import (
	"testing"
)

// f8 编码严格对齐官服抓包 24/19：(branch<<16)|stage。
func TestQualTitleValueEncoding(t *testing.T) {
	quals := []profileQualification{
		{branch: 1, stage: 7},  // 纺车竿 7 级 → 0x10007
		{branch: 4, stage: 7},  // 手竿 7 级   → 0x40007
		{branch: 6, stage: 2},  // 飞德 2 级   → 0x60002
	}
	cases := []struct {
		name   string
		chosen int64
		want   uint32
	}{
		{"分支1等级7", 1, 0x10007},
		{"分支4等级7", 4, 0x40007},
		{"分支6等级2", 6, 0x60002},
		{"未选自动取最高级同级分支大", 0, 0x40007},
	}
	for _, tc := range cases {
		if got := qualTitleValue(quals, tc.chosen); got != tc.want {
			t.Errorf("%s: got 0x%x, want 0x%x", tc.name, got, tc.want)
		}
	}
	// 选了未通过的分支 → 回退自动，不能下发不存在的称号。
	if got := qualTitleValue(quals, 8); got != 0x40007 {
		t.Errorf("选未通过分支应回退自动: got 0x%x", got)
	}
	if got := qualTitleValue(nil, 4); got != 0 {
		t.Errorf("无资格应返回 0: got 0x%x", got)
	}
}

// 管理指令 set_qual_title → kv 持久化 → currentQualTitle 读回，
// 保证重登后 f8 仍是玩家/管理员选定的分支，不会重置为纺车竿（分支1）。
func TestQualTitlePersistsAcrossRelogin(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	ctx := t.Context()
	uid := int64(1000001)

	// 构造两个已通过分支：分支1等级1、分支4等级7。
	// 用真实资格状态表 player_qualifications，ruleID = branch*1000 + (stage-1)*100 + task。
	for stage := int64(1); stage <= 7; stage++ {
		for task := int64(1); task <= 3; task++ {
			if _, err := db.SQL.ExecContext(ctx,
				`INSERT OR REPLACE INTO player_qualifications(uid,rule_id,counted_json,completed_ts) VALUES(?,?,'[]',?)`,
				uid, 4*1000+(stage-1)*100+task, float64(1700000000+stage)); err != nil {
				t.Fatalf("写分支4进度失败: %v", err)
			}
		}
	}
	for task := int64(1); task <= 3; task++ {
		if _, err := db.SQL.ExecContext(ctx,
			`INSERT OR REPLACE INTO player_qualifications(uid,rule_id,counted_json,completed_ts) VALUES(?,?,'[]',?)`,
			uid, 1*1000+task, float64(1700000001)); err != nil {
			t.Fatalf("写分支1进度失败: %v", err)
		}
	}

	// 默认自动：应取等级最高的分支4，不是分支1纺车竿。
	auto := srv.currentQualTitle(ctx, uid)
	if auto>>16 != 4 {
		t.Fatalf("自动称号应取分支4(0x%x), got 0x%x", auto, auto)
	}

	// 管理员设为分支1（纺车竿）：重登（重新构造 currentQualTitle 调用）仍应是分支1。
	if err := srv.setQualTitle(ctx, uid, 1); err != nil {
		t.Fatalf("设置称号失败: %v", err)
	}
	got := srv.currentQualTitle(ctx, uid)
	if got>>16 != 1 || got&0xffff != 1 {
		t.Fatalf("持久化后应显示分支1等级1(0x10001), got 0x%x", got)
	}

	// 清除（0=自动）：回到分支4。
	if err := srv.setQualTitle(ctx, uid, 0); err != nil {
		t.Fatalf("清除称号失败: %v", err)
	}
	got = srv.currentQualTitle(ctx, uid)
	if got>>16 != 4 {
		t.Fatalf("清除后应回到自动分支4(0x%x), got 0x%x", auto, got)
	}
}
