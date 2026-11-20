package gameserver

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"rf4server/internal/proto"
)

// TestProfile307HidesTripPurchase：用户补丁——档案 307 f22 虚拟注入铜湖/挪威海
// 的"永久买断"条目，让客户端隐藏钓鱼游购买页（页面由客户端本地静态渲染，
// 只有 f22 能影响显示）。开关 trip_purchase_hidden=0 时恢复原样。
func TestProfile307HidesTripPurchase(t *testing.T) {
	db := connectTestDB(t)
	srv := NewServerForGearTest(t, db)
	ctx := context.Background()
	const uid = int64(1000001)
	if _, err := db.EnsureProfile(ctx, uid); err != nil {
		t.Fatal(err)
	}

	f22Places := func() map[string]bool {
		t.Helper()
		p, err := srv.BuildProfile307(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		f := p.Fields[22].Val
		if f == nil || f.Kind != proto.KindArray {
			t.Fatalf("307.f22 应为字节数组，实得 %v", f)
		}
		for _, item := range f.Items {
			if item == nil || item.Hex == "" {
				continue
			}
			b, err := hex.DecodeString(strings.TrimPrefix(item.Hex, "0x"))
			if err == nil {
				out[string(b)] = true
			}
		}
		return out
	}

	// 默认开启：两地图都在 f22。
	places := f22Places()
	for _, want := range []string{tripHiddenPlaceCopper, tripHiddenPlaceNorthSea} {
		if !places[want] {
			t.Fatalf("f22 应含 %s（隐藏购买页），实得 %v", want, places)
		}
	}

	// 关闭开关：不再虚拟注入（玩家库无 buyout 记录）。
	srv.tunMu.Lock()
	if srv.tunCache == nil {
		srv.tunCache = map[string]float64{}
	}
	srv.tunCache["trip_purchase_hidden"] = 0
	srv.tunMu.Unlock()
	places = f22Places()
	for _, pl := range []string{tripHiddenPlaceCopper, tripHiddenPlaceNorthSea} {
		if places[pl] {
			t.Fatalf("关闭开关后 f22 不应含 %s", pl)
		}
	}
}
