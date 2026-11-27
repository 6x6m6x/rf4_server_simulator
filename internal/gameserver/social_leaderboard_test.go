package gameserver

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"testing"

	"rf4server/internal/proto"
)

// 24/21 绝对记录榜：按官服实包形状返回全服钓获，不再空数组。
// 官服证据：顶层 7 键，每鱼种 Top5，每条 7 字段，user 9 字段。
func TestSocialAbsoluteRecordsFromCatchLog(t *testing.T) {
	env := newDLineTestEnv(t, 100)
	registerSocialHandlers(env.srv.dispatch)
	ctx := context.Background()
	for _, uid := range []int64{201, 202} {
		if _, err := env.db.EnsureProfile(ctx, uid); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := env.db.SQL.Exec(`UPDATE profile SET name='钓友甲',level=42,avatar='https://a.test/1.jpg' WHERE uid=201`); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.SQL.Exec(`UPDATE profile SET name='钓友乙',level=31 WHERE uid=202`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO loc_alias(alias,loc) VALUES('level_018_moskitnoe','铜湖')`,
		`INSERT INTO item_catalog(item_id,name_zh) VALUES('boil_test','测试煮饵')`,
		`INSERT INTO catch_log(ts,loc,fish_key,name_zh,weight_g,source,bait,xp,loc_level,uid)
		 VALUES(1700000000,'level_018_moskitnoe','s_freaks','怪胎',9000,'keepnet','boil_test',300,'level_018_moskitnoe',201)`,
		`INSERT INTO catch_log(ts,loc,fish_key,name_zh,weight_g,source,bait,xp,loc_level,uid)
		 VALUES(1700000100,'level_018_moskitnoe','s_freaks','怪胎',8000,'keepnet','boil_test',250,'level_018_moskitnoe',202)`,
	} {
		if _, err := env.db.SQL.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	v := env.invoke(t, 24, 21, proto.Args524(
		proto.StrVal("absolute"), proto.U32(4), proto.StrVal("DEF"), proto.StrVal("zh-CN")))
	raw, err := hex.DecodeString(v.Hex)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Success  bool   `json:"success"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Locale   string `json:"locale"`
		Region   string `json:"region"`
		Modified int64  `json:"modified"`
		Records  []struct {
			Fish struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"fish"`
			Records []map[string]any `json:"records"`
		} `json:"records"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("24/21 非官服 JSON: %v", err)
	}
	if !payload.Success || payload.ID != "absolute" || payload.Type != "DEF" ||
		payload.Locale != "zh-CN" || payload.Region != "CN" || payload.Modified <= 0 {
		t.Fatalf("24/21 顶层字段不符: %+v", payload)
	}
	if len(payload.Records) == 0 || len(payload.Records[0].Records) != 2 {
		t.Fatalf("24/21 records 应来自全服钓获: %+v", payload.Records)
	}
	r0 := payload.Records[0].Records[0]
	if r0["weight"] != float64(9000) || r0["date"] != float64(1700000000) || r0["exp"] != float64(300) {
		t.Fatalf("重量/日期/经验不符官服样本: %+v", r0)
	}
	loc, _ := r0["location"].(map[string]any)
	if loc["id"] != "level_018_moskitnoe" || loc["name"] != "铜湖" {
		t.Fatalf("地点不符: %+v", loc)
	}
	bait, _ := r0["bait_lure"].(map[string]any)
	if bait["id"] != "boil_test" || bait["name"] != "测试煮饵" {
		t.Fatalf("饵不符: %+v", bait)
	}
	user, _ := r0["user"].(map[string]any)
	if user["name"] != "钓友甲" || user["avatar"] != "https://a.test/1.jpg" || user["id"] != float64(201) {
		t.Fatalf("玩家信息不符: %+v", user)
	}
}

// 24/24 明星鱼：官服顶层 4 键（success/modified/placeid/namedfish），
// history 5 字段（id/weight/kg/length/m/baitID/catched）。
func TestSocialNamedFishOfficialShape(t *testing.T) {
	env := newDLineTestEnv(t, 100)
	registerSocialHandlers(env.srv.dispatch)
	ctx := context.Background()
	if _, err := env.db.SQL.Exec(`INSERT INTO named_fish(fish_id,species,name,owner,place,weight_g,length_m,ts)
		VALUES('nf-0001','s_freaks','大怪胎','201','level_018_moskitnoe',9000,0.72,1700000000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.SQL.Exec(`INSERT INTO catch_log(ts,loc,fish_key,name_zh,weight_g,length_m,source,bait,loc_level,uid)
		VALUES(1700000000,'level_018_moskitnoe','s_freaks','怪胎',9000,0.72,'keepnet','boil_test','level_018_moskitnoe',201)`); err != nil {
		t.Fatal(err)
	}
	_ = ctx

	v := env.invoke(t, 24, 24, proto.Args524(proto.StrVal("level_018_moskitnoe")))
	raw, err := hex.DecodeString(v.Hex)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Success   bool   `json:"success"`
		Modified  int64  `json:"modified"`
		PlaceID   string `json:"placeid"`
		NamedFish []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			SystemID string `json:"systemID"`
			History  []struct {
				ID      int64   `json:"id"`
				Weight  float64 `json:"weight"`
				Length  float64 `json:"length"`
				BaitID  string  `json:"baitID"`
				Catched int64   `json:"catched"`
			} `json:"history"`
		} `json:"namedfish"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("24/24 非官服 JSON: %v", err)
	}
	if !payload.Success || payload.Modified <= 0 || payload.PlaceID != "level_018_moskitnoe" {
		t.Fatalf("24/24 顶层 4 键不符: %+v", payload)
	}
	if len(payload.NamedFish) != 1 || payload.NamedFish[0].ID != "nf-0001" ||
		payload.NamedFish[0].SystemID != "s_freaks" || len(payload.NamedFish[0].History) == 0 {
		t.Fatalf("24/24 namedfish 不符: %+v", payload.NamedFish)
	}
	h := payload.NamedFish[0].History[0]
	if h.Weight != 9 || h.Length != 0.72 || h.BaitID != "boil_test" || h.Catched != 1700000000 {
		t.Fatalf("history 数值不符官服单位（kg/m/Unix秒）: %+v", h)
	}
}
