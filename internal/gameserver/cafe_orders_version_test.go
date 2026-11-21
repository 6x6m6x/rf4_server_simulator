package gameserver

import (
	"context"
	"testing"
)

// TestCafePoolMissingGradeLines：用户补丁——鱼种档位线缺失的图（沃尔霍夫河/北顿涅茨河）
// 也能生成候选订单，不因整图鱼被过滤而没有咖啡厅订单。
func TestCafePoolMissingGradeLines(t *testing.T) {
	srv, db := newStaticRigTestServer(t)
	ctx := context.Background()
	const level = "level_005_volhov"
	loc := db.ResolveStatLoc(ctx, level)
	// 抹掉该图所有鱼的档位线，模拟老玩家库数据缺失。
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM fish_grade_lines WHERE fish_key IN
        (SELECT fish_key FROM loc_fish WHERE loc=?)`, loc); err != nil {
		t.Fatal(err)
	}
	counts, err := srv.RegenerateCafeBountyPool(ctx)
	if err != nil {
		t.Fatalf("缺线不应让整体失败: %v", err)
	}
	if counts[level] <= 0 {
		t.Fatalf("缺线的图也应生成候选池，实得 %d", counts[level])
	}
	// 其它图不受影响。
	for _, other := range []string{"level_001_lake", "level_013_sev_don", "level_018_moskitnoe"} {
		if counts[other] <= 0 {
			t.Fatalf("其它水域 %s 也应生成候选池，实得 %d", other, counts[other])
		}
	}
}
