package gameserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"rf4server/internal/store"
)

// seedFishQueryFavorite 给查鱼全流程测试设置后台喜爱饵（新逻辑下没有喜爱饵会提示未设置）。
func seedFishQueryFavorite(t *testing.T, s *Server, db *store.DB, fishZh string) {
	t.Helper()
	if err := db.SaveFishBaitOverride(t.Context(), store.FishBaitOverride{
		FishZh: fishZh, Allow: []string{"蚯蚓"}, Enabled: true,
	}, "test"); err != nil {
		t.Fatal(err)
	}
	s.baitOvCache = nil
}

// 查鱼助手：饵只读后台用户设置的喜爱饵，不播报内置饵；不播报任何钓点。
func TestFishQueryReplyUsesFavoriteBaitsNoBuiltinNoSpot(t *testing.T) {
	result := &FishQueryResponse{AsOf: "2026-11-08T05:38:00Z", Plans: []FishQueryPlan{
		{MapName: "旧奥斯特罗格湖", BaitName: "亮片", RigType: 5, RigName: "路亚", Chance: .1, TheoreticalPerHour: 80,
			References: []FishQueryReference{{Coordinate: "45:12", Catches: 100}}},
		{MapName: "旧奥斯特罗格湖", BaitName: "蚯蚓", RigType: 1, RigName: "浮钓", Chance: .183, TheoreticalPerHour: 65.88,
			EquivalentBaits: 3, References: []FishQueryReference{{Coordinate: "45:12", Catches: 100}}},
		{MapName: "旧奥斯特罗格湖", BaitName: "蛆", RigType: 12, RigName: "底钓", Chance: .125, TheoreticalPerHour: 45,
			References: []FishQueryReference{{Coordinate: "45:12", Catches: 100}}},
	}}
	want := "旧奥斯特罗格湖 · 河鲈\n" +
		"喜爱饵：红虫、玉米\n" +
		"【路亚】\n1. 路亚 · 理论 80.00条/小时\n" +
		"【手杆】\n1. 浮钓 · 理论 65.88条/小时\n" +
		"【底钓】\n1. 底钓 · 理论 45.00条/小时\n" +
		"理论速率不计搏鱼、收鱼时间。"
	if got := formatFishQueryReply("河鲈", result, []string{"红虫", "玉米"}); got != want {
		t.Fatalf("reply = %q; want %q", got, want)
	}
	// 内置饵名和钓点绝不能出现。
	for _, banned := range []string{"亮片", "蚯蚓", "蛆", "45:12", "参考点", "落点", "声呐", "钓点", "区域"} {
		if strings.Contains(want, banned) {
			t.Fatalf("favorite-bait reply should not contain %q", banned)
		}
	}
}

// 未设置喜爱饵：明确提示，不回退播报内置饵。
func TestFishQueryReplyNoFavoriteBaits(t *testing.T) {
	result := &FishQueryResponse{Plans: []FishQueryPlan{{MapName: "旧奥斯特罗格湖", BaitName: "亮片", RigType: 5, TheoreticalPerHour: 80}}}
	got := formatFishQueryReply("河鲈", result, nil)
	if !strings.Contains(got, "该鱼未设置喜爱饵") || strings.Contains(got, "亮片") {
		t.Fatalf("no-favorite reply should prompt setup and never show builtin bait: %s", got)
	}
}

func TestFishQueryReplyNeverShowsSpot(t *testing.T) {
	x, z := 425.0, 495.0
	result := &FishQueryResponse{Plans: []FishQueryPlan{
		{MapName: "测试湖", BaitName: "饵一", RigType: 5, RigName: "路亚", Action: "jig", Chance: .1, X: &x, Z: &z,
			CoordinateKind: "manual", References: []FishQueryReference{{Coordinate: "99:99"}}},
	}}
	reply := formatFishQueryReply("河鲈", result, []string{"喜爱饵"})
	if !strings.Contains(reply, "喜爱饵：喜爱饵") || !strings.Contains(reply, "路亚·阶梯") {
		t.Fatalf("favorite bait and method should remain: %s", reply)
	}
	for _, banned := range []string{"世界落点", "425.0", "495.0", "声呐", "米钓点", "99:99", "饵一"} {
		if strings.Contains(reply, banned) {
			t.Fatalf("spot or builtin bait leaked into reply (%q): %s", banned, reply)
		}
	}
	// 海图区/声呐区/米钓点一律不播报。
	result.Plans[0].MapName, result.Plans[0].Zone, result.Plans[0].CoordinateKind = "挪威海", 7, "sea_grid"
	result.Plans[0].Map, result.Plans[0].Zone = "level_012_north_sea", 252
	x, z = 3282, -925
	reply = formatFishQueryReply("剑鱼", result, []string{"喜爱饵"})
	if strings.Contains(reply, "声呐区") || strings.Contains(reply, "米钓点") || strings.Contains(reply, "世界落点") {
		t.Fatalf("chart spot should not be shown: %s", reply)
	}
}

// 无钓法方案但已设喜爱饵：仍要报出喜爱饵，不能只回「当前地图没有可行方案」。
func TestFishQueryReplyNoPlanStillShowsFavoriteBaits(t *testing.T) {
	result := &FishQueryResponse{}
	got := formatFishQueryReply("红白锦鲤", result, []string{"玉米", "红虫"})
	want := "红白锦鲤\n喜爱饵：玉米、红虫\n当前地图、当前条件没有可行方案，可指定其他地图。"
	if got != want {
		t.Fatalf("no-plan reply = %q; want %q", got, want)
	}
}

func TestFishQueryReplyLimitedAndNoInventedCoordinate(t *testing.T) {
	result := &FishQueryResponse{}
	if got := formatFishQueryReply("河鲈", result, nil); !strings.Contains(got, "该鱼未设置喜爱饵") {
		t.Fatalf("empty reply = %s", got)
	}
	for range 4 {
		result.Plans = append(result.Plans, FishQueryPlan{MapName: "测试湖", BaitName: "饵", RigType: 1, RigName: "浮钓", Chance: .001, TheoreticalPerHour: .36, Action: "drift"})
	}
	got := formatFishQueryReply("河鲈", result, []string{"喜爱饵"})
	if strings.Contains(got, "4.") || !strings.Contains(got, "3. 浮钓·顺水漂流 · 理论 0.36条/小时") ||
		!strings.Contains(got, "仅列出当前条件下有可行方案的钓法。") ||
		strings.Contains(got, "参考点") || strings.Contains(got, "落点") || strings.Contains(got, "内置") ||
		strings.Contains(got, "喜爱饵：饵\n") {
		t.Fatalf("too many plans, unnecessary fields or invented position: %s", got)
	}
}

func TestFishQueryHelpWaitNotice(t *testing.T) {
	if !strings.Contains(fishQueryHelp, "约3秒") || !strings.Contains(fishQueryHelp, "个人消息返回") {
		t.Fatalf("help does not explain query wait: %s", fishQueryHelp)
	}
}

func TestFishQueryReplyExplanationsMovedToHelp(t *testing.T) {
	result := &FishQueryResponse{WaterFiltered: true, Plans: []FishQueryPlan{{MapName: "铜湖", BaitName: "饵", RigType: 5, CoordinateKind: "inland_region", Region: &FishQueryRegion{MinX: 40, MinZ: 48, MaxX: 48, MaxZ: 56}}}}
	got := formatFishQueryReply("绯写锦鲤", result, []string{"喜爱饵"})
	for _, detail := range []string{"粗筛", "不保证", "五点", "混合格", "预测", "单竿"} {
		if strings.Contains(got, detail) || !strings.Contains(fishQueryHelp, detail) {
			t.Fatalf("detail %q should be help-only: reply=%s help=%s", detail, got, fishQueryHelp)
		}
	}
	if !strings.HasSuffix(got, "理论速率不计搏鱼、收鱼时间。") {
		t.Fatalf("missing short rate explanation: %s", got)
	}
}

func TestFishQuerySpeciesOnlyHelp(t *testing.T) {
	if !strings.HasPrefix(fishQueryHelp, "发送：鱼名 [地图]\n") {
		t.Fatalf("help does not describe species-only syntax: %s", fishQueryHelp)
	}
	for _, removed := range []string{"普通", "达标", "星", "奖杯", "蓝", "档位"} {
		if strings.Contains(fishQueryHelp, removed) {
			t.Fatalf("help still advertises grade %q: %s", removed, fishQueryHelp)
		}
	}
}

func TestFishQuerySpeciesOnlyCommands(t *testing.T) {
	s, db := seedBaitWeightTest(t)
	// 查鱼助手只播报后台设置的喜爱饵。
	seedFishQueryFavorite(t, s, db, "常见拟鲤")
	const uid int64 = 1000001
	const mapKey = "level_015_tunguska"
	if _, err := db.SQL.Exec(`UPDATE loc_alias SET alias=? WHERE alias='level_900_rules'`, mapKey); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureProfile(t.Context(), uid); err != nil {
		t.Fatal(err)
	}
	s.setLevel(uid, mapKey)
	sess := &Session{Srv: s, Userid: uid}
	for _, text := range []string{"常见拟鲤", "常见拟鲤 rules-test", "查鱼 常见拟鲤", "查询 常见拟鲤 " + mapKey} {
		t.Run(text, func(t *testing.T) {
			got := s.fishQueryReply(t.Context(), sess, text)
			if !strings.HasPrefix(got, "rules-test · 常见拟鲤\n喜爱饵：") || !strings.Contains(got, "\n【") || !strings.Contains(got, "\n1. ") || strings.Contains(got, "及以上") {
				t.Fatalf("species-only query returned %q", got)
			}
		})
	}
	for _, grade := range []string{"普通", "达标", "星", "奖杯", "蓝", "蓝冠"} {
		t.Run("removed/"+grade, func(t *testing.T) {
			if got := s.fishQueryReply(t.Context(), sess, "常见拟鲤 "+grade); got != "地图未识别，请用地图全名或level编号" {
				t.Fatalf("grade suffix is still interpreted as a filter: %q", got)
			}
			if got := s.fishQueryReply(t.Context(), sess, "常见拟鲤 "+mapKey+" "+grade); got != fishQueryHelp {
				t.Fatalf("extra grade argument is still accepted: %q", got)
			}
		})
	}
}

func TestFishQueryReplyExpiredDeadline(t *testing.T) {
	s, _ := seedBaitWeightTest(t)
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	// A timeout during name resolution must not be reported as unavailable data.
	got := s.fishQueryReply(ctx, &Session{Srv: s, Userid: 1000001}, "常见拟鲤")
	if want := "查询超时（计算预算3秒），请稍后重试。"; got != want {
		t.Fatalf("timeout reply=%q, want %q", got, want)
	}
}
