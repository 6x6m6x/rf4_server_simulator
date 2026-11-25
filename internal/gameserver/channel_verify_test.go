package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/proto"
)

// 24/5 频道列表：官服快照 18160 项，空搜索返回非空 args(137)。
func TestOfficialChatListSnapshot(t *testing.T) {
	env := newDLineTestEnv(t, 100)
	registerSocialHandlers(env.srv.dispatch)
	_ = context.Background()
	all := loadOfficialChatChannels(nil)
	if len(all) != 18160 { t.Fatalf("频道快照数=%d 期望18160", len(all)) }
	v := env.invoke(t, 24, 5, proto.Args524(proto.StrVal(""), proto.StrVal("")))
	if v.Kind != proto.KindArgs || v.Code == nil || *v.Code != "137" { t.Fatalf("24/5 非 args(137): %+v", v) }
	if len(v.Items) == 0 { t.Fatal("24/5 空搜索返回空列表") }
	if v.Items[0].Class != "godhiicbgng" { t.Fatalf("首项非 obj402: %+v", v.Items[0]) }
	// 关键字过滤
	v2 := env.invoke(t, 24, 5, proto.Args524(proto.StrVal("nav19_01"), proto.StrVal("")))
	if len(v2.Items) == 0 || len(v2.Items) >= len(v.Items) { t.Fatalf("关键字过滤无效: all=%d filtered=%d", len(v.Items), len(v2.Items)) }
}
