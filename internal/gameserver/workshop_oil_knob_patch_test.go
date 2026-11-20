package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// TestOilCompatOverrideFallback：品类10润滑油优先读 compat 表；配置了用它，
// 未配置（或空）回退服务端原逻辑（全部 cat10 油）。摇柄(34)不提供替代型号。
func TestOilCompatOverrideFallback(t *testing.T) {
	// compatTable 是包级全局，本测试临时改写，不能与其它用例并行。
	srv, _ := newRigTestServer(t)
	ctx := context.Background()
	if _, _, err := srv.LoadCompatTable(); err != nil {
		t.Fatal(err)
	}
	reel, err := srv.CreateItem(ctx, 1000001, "spin_5820", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	parts := srv.repairPartsOf(ctx, reel)
	var oil, knob *repairPart
	for i := range parts {
		if parts[i].Cat == 10 {
			oil = &parts[i]
		}
		if parts[i].Cat == 34 {
			knob = &parts[i]
		}
	}
	if oil == nil {
		t.Fatal("缺少润滑油零件")
	}
	if knob == nil {
		t.Fatal("缺少摇柄零件（维修功能必须保留）")
	}

	// 摇柄：只返回本件，不提供替代型号（客户端无"选择替代品"入口）。
	knobAlts := srv.partAlternatives(ctx, reel.ItemID, *knob)
	if len(knobAlts) != 1 {
		t.Fatalf("摇柄替代品应只有本件1个, got=%d", len(knobAlts))
	}

	// 备份原表，测试后恢复。
	orig := compatTable
	defer func() { compatTable = orig }()

	// 未配置 cat10：走原逻辑，返回全部油（>3）。
	base := map[string]map[string][]string{}
	for k, v := range orig {
		cp := map[string][]string{}
		for cat, ids := range v {
			cp[cat] = append([]string(nil), ids...)
		}
		base[k] = cp
	}
	delete(base["spin_5820"], "10")
	compatTable = base
	if got := len(srv.partAlternatives(ctx, reel.ItemID, *oil)); got < 3 {
		t.Fatalf("未配置cat10应回退全部油, got=%d", got)
	}

	// 配置 cat10 两个油：只用这两个（+本件自身，若其中之一恰为本件则不重复）。
	base["spin_5820"]["10"] = []string{"oil_aquatica", "msh_reel_oil"}
	compatTable = base
	got := srv.partAlternatives(ctx, reel.ItemID, *oil)
	if len(got) != 3 {
		t.Fatalf("配置2个油应返回本件+2=3, got=%d", len(got))
	}
}

// TestWorkshopHidesKnobInUIDiag：21/2 诊断不展示摇柄(34)整行，
// 但 repairPartsOf 仍返回它（21/4 维修功能不下）。
func TestWorkshopHidesKnobInUIDiag(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}
	reel, err := srv.CreateItem(ctx, uid, "spin_5820", "backpack", 1)
	if err != nil {
		t.Fatal(err)
	}
	parts := srv.repairPartsOf(ctx, reel)
	knobGUID := ""
	for _, p := range parts {
		if p.Cat == 34 {
			knobGUID = p.GUID
		}
	}
	if knobGUID == "" {
		t.Fatal("repairPartsOf 仍应返回摇柄（维修功能保留）")
	}
	if !workshopHiddenInUI(34) || workshopHiddenInUI(7) {
		t.Fatal("隐藏判定错误")
	}

	diagBody, ok := hWorkshopDiag(&Ctx{
		Sess: newProbeSess(srv),
		Tree: proto.Args524(proto.Guid(reel.GUID)),
		Log:  logging.Discard(),
	})
	if !ok || len(diagBody) == 0 {
		t.Fatal("21/2 未返回诊断")
	}
	diag, _, err := proto.DecodeValue(diagBody, 0, srv.Table())
	if err != nil {
		t.Fatal(err)
	}
	comps := diag.Field(1).Val
	if comps == nil {
		t.Fatal("21/2 缺少内件清单")
	}
	for _, item := range comps.Items {
		gf := item.Field(0)
		if gf.GuidHex() == knobGUID {
			t.Fatal("21/2 不应展示摇柄(34)整行")
		}
	}
}
