package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/proto"
)

// 官服抓包 2026-11-25 校准：24/18 顶层必须含 f1(obj322 统计) 与 f2(obj321 鱼类记录)，
// 缺字段会让客户端打开他人资料卡时抛错、页面空白。
func TestSocialUserProfileHasOfficialFields(t *testing.T) {
	env := newDLineTestEnv(t, 100)
	registerSocialHandlers(env.srv.dispatch)
	ctx := context.Background()
	if _, err := env.db.EnsureProfile(ctx, 200); err != nil { t.Fatal(err) }

	card := decodeWrappedBytes(t, env.invoke(t, 24, 18, proto.Args524(proto.U32(200))))
	if card.Class != "feaidppnpch" { t.Fatalf("24/18 class=%q", card.Class) }
	if len(card.Fields) < 10 { t.Fatalf("24/18 字段数=%d 期望>=10", len(card.Fields)) }
	if card.Fields[1].Val == nil || card.Fields[1].Val.Kind != proto.KindObj || card.Fields[1].Val.Class != "kkmhfbeldpg" {
		t.Fatalf("24/18.f1 缺 obj322 统计: %+v", card.Fields[1])
	}
	if card.Fields[2].Val == nil || card.Fields[2].Val.Kind != proto.KindObj || card.Fields[2].Val.Class != "jahbacemfof" {
		t.Fatalf("24/18.f2 缺 obj321 鱼类记录: %+v", card.Fields[2])
	}
}

// 24/4 联系人必须包含所有已建档角色（离线也可见），否则玩家之间无法互相打开资料卡。
func TestSocialMembersIncludeOfflinePlayers(t *testing.T) {
	env := newDLineTestEnv(t, 100)
	registerSocialHandlers(env.srv.dispatch)
	for _, uid := range []int64{301, 302, 303} {
		if _, err := env.db.EnsureProfile(context.Background(), uid); err != nil { t.Fatal(err) }
	}
	v := env.invoke(t, 24, 4, proto.Args524(proto.U32(0), proto.StrVal("")))
	if v.Kind != proto.KindU32Arr { t.Fatalf("24/4 非 u32arr: %+v", v) }
	got := map[uint64]bool{}
	for _, it := range v.Items { got[it.Num] = true }
	for _, uid := range []int64{301, 302, 303} {
		if !got[uint64(uid)] { t.Fatalf("24/4 缺离线玩家 %d: %+v", uid, v.Items) }
	}
}
