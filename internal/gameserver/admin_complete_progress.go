package gameserver

import (
	"fmt"
	"time"

	"rf4server/internal/store"
)

// 管理后台「玩家 → 概况」里的一键完成：管理员专用，不走玩家自助入口。
//
// 两个入口都由 execAdminCmd 调度（共享 SQLite 管理指令队列），因此规则 ID
// 直接取本包的权威来源：成就取 achievement_def 全表，资格取 qualificationRules
// （跳过 qGapCounted 缺口任务——那些任务当前没有判定信号，面板不下发，不能点亮）。
//
// 设计要点：
//   - 幂等：重复执行只补齐缺失项；奖杯由 ensureQualificationCups 的 kv 幂等键保证不重发。
//   - 达成态完整：成就 unlocked+claimed 一起写（客户端只有 claimed=1 点亮）；
//     资格的 Counted 填到 qualificationTarget 条并写 CompletedTS，进度条与完成时间都正确。
//   - 在线会话补推 3/3 与 2/8 解锁提示；资格面板（28/1）下次打开自动读完成态。

// completeAllAchievementsForAdmin 一键点亮该玩家全部成就。
func (s *Server) completeAllAchievementsForAdmin(uid int64) int {
	if s == nil || s.DB == nil || uid <= 0 {
		return 0
	}
	ctx := s.dbCtx()
	ids, err := s.DB.AchievementDefinitions(ctx)
	if err != nil {
		s.log.Warn("管理指令：一键完成成就读定义失败", "uid", uid, "err", err)
		return 0
	}
	now := float64(time.Now().Unix())
	newly := make([]int64, 0, len(ids))
	for _, id := range ids {
		claimed, err := s.DB.UnlockAndClaimAchievement(ctx, uid, id, 1, now)
		if err != nil {
			s.log.Warn("管理指令：一键完成成就写入失败", "uid", uid, "id", id, "err", err)
			continue
		}
		if claimed {
			newly = append(newly, id)
		}
	}
	// 在线会话补推 3/3 统计快照与 2/8 解锁提示（管理指令没有请求 Ctx，
	// 不复用 queueAchievementUnlockPushes 的 AfterResponse，直接遍历会话）。
	for _, sess := range s.sessionsForUID(uid) {
		sess.Push2_1()
		for _, id := range newly {
			sess.pushAchievementUnlocked(id)
		}
	}
	s.log.Info("管理指令：一键完成成就", "uid", uid, "total", len(ids), "newly", len(newly))
	return len(newly)
}

// completeAllQualificationsForAdmin 一键完成该玩家全部资格任务（含活动分支）。
func (s *Server) completeAllQualificationsForAdmin(uid int64) int {
	if s == nil || s.DB == nil || uid <= 0 {
		return 0
	}
	ctx := s.dbCtx()
	now := float64(time.Now().Unix())
	done := 0
	for _, r := range qualificationRules {
		if eventGapRuleIDs[r.ID] {
			continue // 缺口任务无判定信号，面板不下发，不能点亮
		}
		target := int(qualificationTarget(uint64(r.ID)))
		if target < 1 {
			target = 1
		}
		fish := make([]store.QualificationFish, 0, target)
		for i := 0; i < target; i++ {
			fish = append(fish, store.QualificationFish{
				GUID:    adminQualificationGUID(r.ID, i),
				Species: "admin_complete",
				NameZh:  "管理员补录",
			})
		}
		st := store.QualificationState{RuleID: r.ID, Counted: fish, CompletedTS: now}
		if err := s.DB.UpsertQualificationState(ctx, uid, st); err != nil {
			s.log.Warn("管理指令：一键完成资格写入失败", "uid", uid, "id", r.ID, "err", err)
			continue
		}
		done++
	}
	// 补发全部分支奖杯（kv 幂等，已发过不会重复；未在线也能写库）。
	s.ensureQualificationCups(ctx, uid)
	// 3/3 统计也读资格徽章，补推一次；资格面板 28/1 下次打开自动读完成态。
	for _, sess := range s.sessionsForUID(uid) {
		sess.Push2_1()
	}
	s.log.Info("管理指令：一键完成资格", "uid", uid, "rules", done)
	return done
}

// adminQualificationGUID 给补录的计数项造稳定去重 GUID（32 位十六进制，
// 与正常渔获的 md5 键同格式）。客户端 5004.f1 只把它当 Guid[] 元素，
// 配合 NameZh 渲染任务鱼表，内容对显示无影响。
func adminQualificationGUID(ruleID int64, idx int) string {
	return qualificationCountedGUID(fmt.Sprintf("admin-complete:%d:%d", ruleID, idx))
}
