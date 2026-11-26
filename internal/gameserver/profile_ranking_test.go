package gameserver

import (
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/store"
)

// doneStage 把 branch 的 stage 阶段 3 条 task 全部标记完成，返回 ruleID->state。
func doneStage(dst map[int64]store.QualificationState, branch, stage int64, ts float64) {
	for task := int64(1); task <= qualificationTasks; task++ {
		dst[branch*1000+(stage-1)*100+task] = store.QualificationState{CompletedTS: ts}
	}
}

// TestProfileQualificationEntriesUseActualStage 回归用户明确要求：f7 的 f1 是
// 玩家每种资格连续通过的实际等级（每个阶段 3 条任务全做完才算通过，C 口径），
// 而不是渔获数/排名序号，也不是"完成过的最高 stage"。
func TestProfileQualificationEntriesUseActualStage(t *testing.T) {
	states := map[int64]store.QualificationState{}
	// branch1: stage1/2/3 全完成 → 3 级
	doneStage(states, 1, 1, 1762000000)
	doneStage(states, 1, 2, 1762000100)
	doneStage(states, 1, 3, 1762000200)
	// branch3(传统纺车杆): stage1..7 全完成 → 7 级
	for s := int64(1); s <= 7; s++ {
		doneStage(states, 3, s, 1762000300+float64(s))
	}
	// branch4: stage1 全完成 → 1 级
	doneStage(states, 4, 1, 1762001000)
	// branch2: 只有 stage1 的 1 条 → 0 级，不下发（断档不算通过）
	states[2101] = store.QualificationState{CompletedTS: 1762001100}

	quals := collectProfileQualifications(states)

	wantBadge := map[uint32]uint32{
		qualificationBadgeID(1, 3): 3,
		qualificationBadgeID(3, 7): 7, // 传统纺车杆 7 级 = 257
		qualificationBadgeID(4, 1): 1,
	}
	wantBranch := map[uint32]uint32{1: 3, 3: 7, 4: 1}
	if len(quals) != len(wantBadge) {
		t.Fatalf("资格条目数=%d，want %d: %+v", len(quals), len(wantBadge), quals)
	}
	for _, q := range quals {
		stage, ok := wantBadge[q.badgeID]
		if !ok {
			t.Fatalf("意外徽章 ID %d", q.badgeID)
		}
		if uint32(q.stage) != stage {
			t.Errorf("徽章 %d 等级=%d，want %d", q.badgeID, q.stage, stage)
		}
		if wantStage, ok := wantBranch[uint32(q.branch)]; !ok || uint32(q.stage) != wantStage {
			t.Errorf("分支 %d 等级=%d，want %v", q.branch, q.stage, wantBranch[uint32(q.branch)])
		}
	}

	v := buildProfileQualificationEntries(quals)
	if v == nil || v.Kind != proto.KindArgs || len(v.Items) != len(wantBranch) {
		t.Fatalf("f7 应为 args(207) 且 %d 项，实得 %+v", len(wantBranch), v)
	}
	for _, item := range v.Items {
		if item.Kind != proto.KindObj || item.Class != "iahhndknkbe" {
			t.Fatalf("f7 项应为 obj5010，实得 %+v", item)
		}
		branch := item.Field(0).Num
		stage := item.Field(1).Num
		ts := item.Field(2).Num
		if branch == nil || stage == nil || ts == nil {
			t.Fatalf("f7 字段缺失: %+v", item)
		}
		if *branch < 1 || *branch > uint64(qualificationBranches) {
			t.Errorf("f0=%d 超出分支范围，客户端 qlfctn_n_<分支> 查不到名称/图标", *branch)
		}
		if wantStage, ok := wantBranch[uint32(*branch)]; !ok || *stage != uint64(wantStage) {
			t.Errorf("f7 分支 %d 等级=%d，want %v", *branch, *stage, wantBranch[uint32(*branch)])
		}
		if *ts == 0 {
			t.Errorf("f7 分支 %d FILETIME 为空", *branch)
		}
	}
}

// TestProfileQualificationStageGapStops 断档即停：stage1/2/3/7 各只完成 1 条，
// 因 stage1 未集齐 3 条，整个分支等级=0、不下发（C 口径核心行为）。
func TestProfileQualificationStageGapStops(t *testing.T) {
	states := map[int64]store.QualificationState{
		7101: {CompletedTS: 1762000000}, // stage1 task1 only
		7201: {CompletedTS: 1762000100}, // stage2 task1 only
		7301: {CompletedTS: 1762000200}, // stage3 task1 only
		7703: {CompletedTS: 1762000300}, // stage7 task3 only（完成过的最高 stage=7）
	}
	quals := collectProfileQualifications(states)
	if len(quals) != 0 {
		t.Fatalf("断档分支等级应为0不下发，实得 %+v", quals)
	}
}

// TestProfileQualificationStagePartialThenFull stage1 集齐 3 条、stage2 只有 2 条 →
// 等级停在 1，不是 2；验证"3 条全做完"的严格口径。
func TestProfileQualificationStagePartialThenFull(t *testing.T) {
	states := map[int64]store.QualificationState{}
	doneStage(states, 5, 1, 1762000000) // stage1 全完成
	states[5201] = store.QualificationState{CompletedTS: 1762000100}
	states[5202] = store.QualificationState{CompletedTS: 1762000200}
	// stage2 task3 缺失
	quals := collectProfileQualifications(states)
	if len(quals) != 1 || quals[0].branch != 5 || quals[0].stage != 1 {
		t.Fatalf("应只算 stage1 等级=1，实得 %+v", quals)
	}
}

// TestProfileQualificationEntriesEmpty 无完成资格时 f7 为空数组。
func TestProfileQualificationEntriesEmpty(t *testing.T) {
	states := map[int64]store.QualificationState{
		1001: {CompletedTS: 0},
	}
	quals := collectProfileQualifications(states)
	v := buildProfileQualificationEntries(quals)
	if v == nil || v.Kind != proto.KindArgs || len(v.Items) != 0 {
		t.Fatalf("无完成资格时 f7 应为空 args，实得 %+v", v)
	}
}

// TestProfileQualificationNoSampleBranchKept 分支7（鲤鱼捕钓）/8（海洋钓鱼）虽无
// 官服徽章样本（badgeID=0），但仍须保留在 quals 供 f7 用分支ID下发——否则资料卡
// 资格页只会显示 6 种。f4 已改读已解锁成就（与统计面板一致），与资格 badgeID 无关。
func TestProfileQualificationNoSampleBranchKept(t *testing.T) {
	states := map[int64]store.QualificationState{}
	doneStage(states, 7, 1, 1762000000) // branch7 stage1 全完成
	doneStage(states, 8, 1, 1762000000) // branch8 stage1 全完成
	doneStage(states, 1, 1, 1762000000) // branch1 有徽章样本
	doneStage(states, 9, 1, 1762000000) // branch9 活动分支，不应出现在永久资格
	quals := collectProfileQualifications(states)
	if len(quals) != 3 {
		t.Fatalf("应保留分支 1/7/8 共3项，实得 %+v", quals)
	}
	byBranch := map[int64]profileQualification{}
	for _, q := range quals {
		byBranch[q.branch] = q
	}
	for _, b := range []int64{1, 7, 8} {
		if _, ok := byBranch[b]; !ok {
			t.Errorf("分支 %d 缺失", b)
		}
	}
	if byBranch[7].badgeID != 0 || byBranch[8].badgeID != 0 {
		t.Errorf("分支7/8 应无徽章ID(badgeID=0)，实得 7=%d 8=%d", byBranch[7].badgeID, byBranch[8].badgeID)
	}
	if _, ok := byBranch[9]; ok {
		t.Error("活动分支 9 不应出现在永久资格汇总")
	}
	v := buildProfileQualificationEntries(quals)
	if v == nil || len(v.Items) != 3 {
		t.Fatalf("f7 应含 3 项（含分支7/8），实得 %+v", v)
	}
	for _, item := range v.Items {
		branch := item.Field(0).Num
		if branch == nil || (*branch != 7 && *branch != 8 && *branch != 1) {
			t.Errorf("f7 出现了意外分支 %v", branch)
		}
	}
}

// TestProfileQualificationStageCapDirtyData 脏数据 stage>7 不下发，避免非法等级
// 被客户端当成超范围资格渲染。
func TestProfileQualificationStageCapDirtyData(t *testing.T) {
	states := map[int64]store.QualificationState{
		3701: {CompletedTS: 1762000000}, // branch3 stage=8（rem=701）→ 跳过
	}
	quals := collectProfileQualifications(states)
	if len(quals) != 0 {
		t.Fatalf("stage>7 的脏数据应跳过，实得 %+v", quals)
	}
}

// TestProfileAchievementBadges 回归：24/18.f4 成就徽章墙必须与「统计」3/3 用
// 同一套 achievement_def / player_achievements ID，而不是资格任务的徽章推算值。
// 未解锁的不发；定义表外的已解锁 ID 按升序补齐，避免漏发。
func TestProfileAchievementBadges(t *testing.T) {
	defs := []int64{101, 102, 151, 250, 251}
	states := map[int64]store.AchievementState{
		101: {AchID: 101, Unlocked: true},
		102: {AchID: 102, Unlocked: false},
		151: {AchID: 151, Unlocked: true},
		250: {AchID: 250, Unlocked: true},
		251: {AchID: 251, Unlocked: false},
		999: {AchID: 999, Unlocked: true}, // 定义表外的已解锁成就也不能丢
	}
	v := buildProfileAchievementBadges(defs, states)
	if v == nil || v.Kind != proto.KindTypedU32Arr || v.TypeName != "311" {
		t.Fatalf("f4 应为 typed_u32arr(311)，实得 %+v", v)
	}
	want := []uint32{101, 151, 250, 999}
	if len(v.Items) != len(want) {
		t.Fatalf("f4 条目数=%d，want %d: %+v", len(v.Items), len(want), v.Items)
	}
	for i, it := range v.Items {
		if uint32(it.Num) != want[i] {
			t.Errorf("f4[%d]=%d，want %d", i, it.Num, want[i])
		}
	}
}

// TestProfileAchievementBadgesEmpty 无已解锁成就时 f4 仍下发空 typed_u32arr，
// 保持官服字段形状（不能缺字段导致客户端资料卡空白）。
func TestProfileAchievementBadgesEmpty(t *testing.T) {
	v := buildProfileAchievementBadges([]int64{101, 102}, map[int64]store.AchievementState{
		101: {AchID: 101, Unlocked: false},
	})
	if v == nil || v.Kind != proto.KindTypedU32Arr || v.TypeName != "311" || len(v.Items) != 0 {
		t.Fatalf("无成就时 f4 应为空 typed_u32arr(311)，实得 %+v", v)
	}
}
