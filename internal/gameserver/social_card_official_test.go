package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/proto"
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
	// 关键回归：整包必须能被重新编码并解码（此前嵌套 buildStats321 产生非法 marker 0x30，
	// 客户端打开他人资料卡报「内部错误」）。
	outer := env.invoke(t, 24, 18, proto.Args524(proto.U32(200)))
	raw, err := proto.EncodeValue(outer, env.sess.Table())
	if err != nil { t.Fatalf("24/18 编码失败: %v", err) }
	if _, _, err := proto.DecodeValue(raw, 0, wiregen.Table()); err != nil {
		t.Fatalf("24/18 回包解码失败(非法marker): %v", err)
	}
}
