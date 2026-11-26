package gameserver

import (
	"context"
	"encoding/hex"
	"testing"

	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/wiregen"
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
	// 关键回归：编码→外层bytes→内层obj完整解码，10字段VAL非空。
	// 此前嵌套 buildStats321 出非法 marker、f3~f7 零值，客户端报「内部错误」。
	outer := env.invoke(t, 24, 18, proto.Args524(proto.U32(200)))
	raw, err := proto.EncodeValue(outer, env.sess.Table())
	if err != nil { t.Fatalf("24/18 编码失败: %v", err) }
	outerVal, _, err := proto.DecodeValue(raw, 0, wiregen.Table())
	if err != nil { t.Fatalf("24/18 外层解码失败: %v", err) }
	rawInner, _ := hex.DecodeString(outerVal.Hex)
	decoded, _, err := proto.DecodeValue(rawInner, 0, wiregen.Table())
	if err != nil { t.Fatalf("24/18 内层解码失败(非法marker): %v", err) }
	if decoded.Class != "feaidppnpch" || len(decoded.Fields) != 10 {
		t.Fatalf("24/18 结构异常: %s/%d", decoded.Class, len(decoded.Fields))
	}
	for i, f := range decoded.Fields {
		if i == 0 || i == 8 { continue }
		if f.Val == nil { t.Fatalf("24/18.f%d VAL 为空", i) }
	}
}

// 24/18 页签数据：f4 成就ID、f5 奖赏、f6 稀有鱼分组、f7 排名须为官服类型，
// 24/18.f4 必须为空 typed_u32arr(311)：填任何ID都会让客户端成就页乱码（nm41实测）。
func TestSocialProfileF4EmptyForAchievements(t *testing.T) {
	env := newDLineTestEnv(t, 100)
	registerSocialHandlers(env.srv.dispatch)
	ctx := context.Background()
	if _, err := env.db.EnsureProfile(ctx, 200); err != nil { t.Fatal(err) }
	// 即使有已完成资格，f4 仍须为空（成就不乱码优先）。
	if err := env.db.UpsertQualificationState(ctx, 200, store.QualificationState{
		RuleID: 1001, Counted: []store.QualificationFish{}, CompletedTS: 1700000000,
	}); err != nil { t.Fatal(err) }
	card := decodeWrappedBytes(t, env.invoke(t, 24, 18, proto.Args524(proto.U32(200))))
	f4 := card.Fields[4].Val
	if f4 == nil || f4.Kind != proto.KindTypedU32Arr || f4.TypeName != "311" {
		t.Fatalf("f4 应为 typed_u32arr(311): %+v", f4)
	}
	if len(f4.Items) != 0 { t.Fatalf("f4 必须为空（否则成就页乱码），实际 %d 项", len(f4.Items)) }
}
