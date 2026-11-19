package gameserver

import (
	"context"
	"math"
	"net"
	"testing"
	"time"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
	"rf4server/internal/store"
	"rf4server/internal/tunnel"
)

// enableLootBoxRandomRewardForTest 在需要验证原随机抽物品机制的测试里临时
// 关闭 disableLootBoxRandomReward（生产默认 true=固定金币），结束自动恢复。
func enableLootBoxRandomRewardForTest(t *testing.T) {
	t.Helper()
	prev := disableLootBoxRandomReward
	disableLootBoxRandomReward = false
	t.Cleanup(func() { disableLootBoxRandomReward = prev })
}

// forceTrophyForLootTest 让发奖机制测试用上星鱼(Grade=3)触发福袋：
// 修正后福袋只在上星/蓝冠开，合格及以下不开。这里把上星概率设为 1，
// 使原本验证"发什么"(随机物品/金币/幂等/过滤)的测试继续只验证发奖本身。
func forceTrophyForLootTest(t *testing.T, s *Server) {
	t.Helper()
	setWeatherTestTunable(t, s, "loot_box_trophy_chance", "1")
}

// enableLootBox 打开总开关（默认关闭，测试里显式开启）。
func enableLootBox(t *testing.T, srv *Server, db *store.DB) {
	t.Helper()
	if _, err := db.SQL.Exec(`INSERT INTO tunables(key,value) VALUES('loot_box_enabled','1')
		ON CONFLICT(key) DO UPDATE SET value='1'`); err != nil {
		t.Fatalf("开启礼包: %v", err)
	}
	srv.tunMu.Lock()
	srv.tunCache = nil
	srv.tunMu.Unlock()
}

func waitLootItemCount(t *testing.T, db *store.DB, uid int64, itemID string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var got int
		if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, uid, itemID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("礼包物品数量=%d，期望 %d", got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// 礼包：概率 1.0 时必开，奖励落库并返回物品 ID。
func TestLootBoxAlwaysOpensWhenChanceOne(t *testing.T) {
	enableLootBoxRandomRewardForTest(t)
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	sess := newProbeSess(srv)
	enableLootBox(t, srv, db)
	forceTrophyForLootTest(t, srv)

	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.SQL.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(900001,'loot_worm',27,'bait','','{}')`)
	mustExec(`INSERT INTO item_catalog(item_id,name,name_zh,kind,numeric_id) VALUES('loot_worm','Loot Worm','礼包红虫','bait',900001)`)
	mustExec(`INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES('fishing_store_01','loot_worm',100,0,10)`)
	mustExec(`INSERT INTO kv(k,v) VALUES('loot_box_defs','[{"id":"test_box","name":"测试礼包","chance":1.0}]')`)

	got := srv.rollLootBox(ctx, sess, &RolledFish{Species: "c.roach", Grade: 3})
	if got != "loot_worm" {
		t.Fatalf("应开出 loot_worm，实得 %q", got)
	}
	var n int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id='loot_worm'`, sess.Userid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("奖励应落库 1 件，实得 %d", n)
	}
}

// 配置了 rewards 时按权重抽取，不再去商店随机。
func TestLootBoxUsesConfiguredRewards(t *testing.T) {
	enableLootBoxRandomRewardForTest(t)
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	sess := newProbeSess(srv)
	enableLootBox(t, srv, db)
	forceTrophyForLootTest(t, srv)

	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.SQL.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// 用计件类（27 饵料）验证数量：非计件物品每件一个实例，数量恒为 1。
	mustExec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(900002,'loot_bait',27,'bait','','{}')`)
	mustExec(`INSERT INTO item_catalog(item_id,name,name_zh,kind,numeric_id) VALUES('loot_bait','Loot Bait','礼包饵料','bait',900002)`)
	mustExec(`INSERT INTO kv(k,v) VALUES('loot_box_defs','[{"id":"b","name":"礼包","chance":1.0,"rewards":[{"item_id":"loot_bait","qty":2,"weight":100}]}]')`)

	got := srv.rollLootBox(ctx, sess, &RolledFish{Species: "c.trophy", Grade: 3})
	if got != "loot_bait" {
		t.Fatalf("应开出 loot_bait，实得 %q", got)
	}
	var qty int64
	if err := db.SQL.QueryRow(`SELECT qty FROM item WHERE uid=? AND item_id='loot_bait'`, sess.Userid).Scan(&qty); err != nil {
		t.Fatal(err)
	}
	if qty != 2 {
		t.Fatalf("数量应为 2，实得 %d", qty)
	}
}

// 关闭开关后不触发。
func TestLootBoxDisabledByTunable(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	sess := newProbeSess(srv)
	if _, err := db.SQL.Exec(`INSERT INTO tunables(key,value) VALUES('loot_box_enabled','0') ON CONFLICT(key) DO UPDATE SET value='0'`); err != nil {
		t.Fatal(err)
	}
	srv.tunMu.Lock()
	srv.tunCache = nil
	srv.tunMu.Unlock()
	if got := srv.rollLootBox(ctx, sess, nil); got != "" {
		t.Fatalf("关闭后不应开箱，实得 %q", got)
	}
}

// 商店随机池必须排除套盒/礼物袋，以及没有权威 wire 映射、无法正确物化的类别。
func TestLootBoxShopPickFiltersUnsupportedCategories(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()

	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.SQL.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`DELETE FROM shop_item`)
	mustExec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(900003,'loot_only_bundle',55,'bundle','','{}')`)
	mustExec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(900004,'loot_only_gift',73,'gift','','{}')`)
	mustExec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(900005,'loot_only_wallet',43,'wallet','','{}')`)
	mustExec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(900006,'loot_allowed',27,'bait','','{}')`)
	mustExec(`INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES
		('s','loot_only_bundle',10,0,5),
		('s','loot_only_gift',10,0,5),
		('s','loot_only_wallet',10,0,5),
		('s','loot_allowed',10,0,5)`)
	for i := 0; i < 60; i++ {
		id, _, _ := srv.pickRandomShopItem(ctx)
		if id != "loot_allowed" {
			t.Fatalf("随机池只能返回可物化商品，实得 %q", id)
		}
	}
}

// 配置奖励与载体也必须走同一套资格校验。
func TestLootBoxConfiguredRewardsFilterUnsupportedItems(t *testing.T) {
	enableLootBoxRandomRewardForTest(t)
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	sess := newProbeSess(srv)
	enableLootBox(t, srv, db)
	forceTrophyForLootTest(t, srv)

	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.SQL.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(900007,'loot_bad_gift',73,'gift','','{}')`)
	mustExec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(900008,'loot_bad_wallet',43,'wallet','','{}')`)
	mustExec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(900009,'loot_good_bait',27,'bait','','{}')`)
	mustExec(`INSERT INTO kv(k,v) VALUES('loot_box_defs','[{"id":"b","name":"礼包","chance":1.0,"carrier_item":"loot_bad_gift","rewards":[{"item_id":"loot_bad_gift","qty":1,"weight":100},{"item_id":"loot_bad_wallet","qty":1,"weight":100},{"item_id":"loot_good_bait","qty":1,"weight":1}]}]')`)

	if got := srv.rollLootBox(ctx, sess, &RolledFish{Species: "c.trophy", Grade: 3}); got != "loot_good_bait" {
		t.Fatalf("应跳过不可发放配置并开出 loot_good_bait，实得 %q", got)
	}
	for _, itemID := range []string{"loot_bad_gift", "loot_bad_wallet"} {
		var n int
		if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id=?`, sess.Userid, itemID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("不可发放物品 %s 不应落库，实得 %d 件", itemID, n)
		}
	}
}

// 14/5 重放不得重复发礼包。
func TestLootBoxAwardsOnceOnKeptReplay(t *testing.T) {
	enableLootBoxRandomRewardForTest(t)
	srv, db := newDetachTestServer(t)
	ctx := context.Background()
	const uid = int64(1000001)
	enableLootBox(t, srv, db)
	forceTrophyForLootTest(t, srv)

	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.SQL.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(900010,'loot_replay',27,'bait','','{}')`)
	mustExec(`INSERT INTO kv(k,v) VALUES('loot_box_defs','[{"id":"b","name":"礼包","chance":1.0,"rewards":[{"item_id":"loot_replay","qty":1,"weight":100}]}]')`)

	fish := &RolledFish{
		FishGUID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Species: "c.roach", Grade: 3,
		WeightG: 186, LengthM: 0.1981, LocLevel: "level_019_american_pond",
	}
	if r := srv.settleCatchXP(ctx, uid, fish, 1); r == nil {
		t.Fatal("settleCatchXP 返回 nil")
	}
	st := srv.stateFor(uid)
	st.mu.Lock()
	st.Pending = fish
	st.Loc, st.Spot, st.Bait = "loc19_03", 123, "worm_01"
	st.mu.Unlock()
	sess := newProbeSess(srv)
	tree := rigRequestTree(t, proto.Guid("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), proto.Guid(fish.FishGUID))
	keepCtx := func() *Ctx {
		return &Ctx{Sess: sess, Main: 14, Sub: 5, Tree: tree, DecodeOK: true, Log: logging.Discard()}
	}

	if body, ok := srv.fishingKeep(keepCtx()); !ok || len(body) == 0 {
		t.Fatal("首次 14/5 未处理")
	}
	waitLootItemCount(t, db, uid, "loot_replay", 1)

	if body, ok := srv.fishingKeep(keepCtx()); !ok || len(body) == 0 {
		t.Fatal("重复 14/5 未处理")
	}
	time.Sleep(150 * time.Millisecond)

	var got int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=? AND item_id='loot_replay'`, uid).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("重复 14/5 后礼包物品应为 1 件，实得 %d", got)
	}
}

// TestLootBoxPushesEveryCouponRow 复审 P2-6：礼包开出多张券时，每一张券行
// 都要推一条 4/22 背包增量。旧实现只推 AddToStack 返回的最后一行。
func TestLootBoxPushesEveryCouponRow(t *testing.T) {
	enableLootBoxRandomRewardForTest(t)
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	if _, err := db.SQL.Exec(`INSERT OR IGNORE INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json)
		VALUES(4825,'coupon_chuchelo',74,'coupon','','{}')`); err != nil {
		t.Fatal(err)
	}
	def, err := db.GetItemDefByItemID(ctx, couponChucheloItemID)
	if err != nil || def == nil {
		t.Skip("静态库无券定义")
	}

	serverNet, peerNet := net.Pipe()
	t.Cleanup(func() { _ = serverNet.Close(); _ = peerNet.Close() })
	_ = serverNet.SetDeadline(time.Now().Add(10 * time.Second))
	_ = peerNet.SetDeadline(time.Now().Add(10 * time.Second))
	ready := make(chan *tunnel.Conn, 1)
	go func() {
		conn, err := tunnel.HandshakeServer(serverNet, srv.cfg.Tunnel.PSK)
		if err == nil {
			ready <- conn
		}
	}()
	peer, err := tunnel.HandshakeClient(peerNet, srv.cfg.Tunnel.PSK)
	if err != nil {
		t.Fatal(err)
	}
	var conn *tunnel.Conn
	select {
	case conn = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("handshake 超时")
	}
	uid := int64(2100040)
	sess := newSession(srv, 1, "loot-coupon-push", uid, conn)
	srv.mu.Lock()
	srv.sessions[1] = sess
	srv.mu.Unlock()

	// 后台持续读帧：net.Pipe 是同步写，没有读端会把 Push 卡死。
	frames := make(chan *proto.Envelope, 64)
	go func() {
		for {
			_, _, payload, err := peer.ReadRecord()
			if err != nil {
				return
			}
			if env, ok := proto.ParseEnvelope(payload); ok {
				frames <- env
			}
		}
	}()

	box := lootBoxDef{ID: "coupon_box", Name: "券礼包", Chance: 1.0,
		Rewards: []lootBoxReward{{ItemID: couponChucheloItemID, Qty: 3, Weight: 100}}}
	if got := srv.grantLootBox(ctx, sess, box, nil, "测试"); got != couponChucheloItemID {
		t.Fatalf("应开出券，实得 %q", got)
	}

	// grantLootBox 末尾还有一条 24/27 聊天推送，只数 4/22 背包增量。
	updates := 0
	deadline := time.After(2 * time.Second)
	collect := true
	for collect {
		select {
		case env := <-frames:
			if env.Main == 4 && env.Sub == 22 {
				updates++
				if updates == 3 {
					collect = false
				}
			}
		case <-deadline:
			collect = false
		}
	}
	if updates != 3 {
		t.Fatalf("应推 3 条 4/22（每张券一行），实得 %d", updates)
	}
	if r, q := couponRowCount(t, srv, uid); r != 3 || q != 3 {
		t.Fatalf("应落 3 行 3 张，实得 %d 行 %d 张", r, q)
	}
}

// readProfileGold 读取玩家金币余额（profile.gold，百分位口径）。
func readProfileGold(t *testing.T, db *store.DB, uid int64) int64 {
	t.Helper()
	var gold int64
	if err := db.SQL.QueryRow(`SELECT gold FROM profile WHERE uid=?`, uid).Scan(&gold); err != nil {
		t.Fatalf("读取金币失败: %v", err)
	}
	return gold
}

// TestLootBoxGoldDefaultRange 固化本次决定：生产默认开箱发随机金币分，
// 金额落在默认范围 [1,200]，不再按权重/商店随机抽物品，也不落任何物品行。
func TestLootBoxGoldDefaultRange(t *testing.T) {
	if !disableLootBoxRandomReward {
		t.Fatal("生产默认应关闭随机奖励（disableLootBoxRandomReward=true）")
	}
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	sess := newProbeSess(srv)
	enableLootBox(t, srv, db)
	forceTrophyForLootTest(t, srv)

	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.SQL.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`INSERT INTO item_def(numeric_id,item_id,category,kind,brand,attrs_json) VALUES(900021,'gold_loot_worm',27,'bait','','{}')`)
	mustExec(`INSERT INTO shop_item(store_id,item_id,silver,gold,stock) VALUES('fishing_store_01','gold_loot_worm',100,0,10)`)
	mustExec(`INSERT INTO kv(k,v) VALUES('loot_box_defs','[{"id":"gold_box","name":"金币礼包","chance":1.0}]')`)
	if err := srv.EnsurePlayer(ctx, sess.Userid); err != nil {
		t.Fatal(err)
	}
	before := readProfileGold(t, db, sess.Userid)
	itemsBefore := countLootItems(t, db, sess.Userid)

	got := srv.rollLootBox(ctx, sess, &RolledFish{Species: "c.roach", Grade: 3})
	if got != "gold" {
		t.Fatalf("随机金币应返回 gold，实得 %q", got)
	}
	delta := readProfileGold(t, db, sess.Userid) - before
	if delta < lootGoldDefaultLo || delta > lootGoldDefaultHi {
		t.Fatalf("金币增量应落在默认范围 [%d,%d]，实得 %d", lootGoldDefaultLo, lootGoldDefaultHi, delta)
	}
	if itemsAfter := countLootItems(t, db, sess.Userid); itemsAfter != itemsBefore {
		t.Fatalf("随机金币不应发放物品，物品数 %d→%d", itemsBefore, itemsAfter)
	}
	// 写了一条金币流水，且金额与档案增量一致。
	var n int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM txn WHERE uid=? AND kind='loot_box' AND gold_delta=?`,
		sess.Userid, delta).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("应写 1 条金额为 %d 的金币流水，实得 %d", delta, n)
	}
}

// TestLootBoxGoldRespectsChance 触发条件不变：概率 0 时不开箱也不加币。
func TestLootBoxFixedGoldRespectsChance(t *testing.T) {
	srv, db := newRigTestServer(t)
	ctx := context.Background()
	sess := newProbeSess(srv)
	enableLootBox(t, srv, db)
	if _, err := db.SQL.Exec(`INSERT INTO kv(k,v) VALUES('loot_box_defs','[{"id":"gold_box","name":"金币礼包","chance":0}]')`); err != nil {
		t.Fatal(err)
	}
	if err := srv.EnsurePlayer(ctx, sess.Userid); err != nil {
		t.Fatal(err)
	}
	before := readProfileGold(t, db, sess.Userid)
	if got := srv.rollLootBox(ctx, sess, &RolledFish{Species: "c.roach"}); got != "" {
		t.Fatalf("概率 0 不应开箱，实得 %q", got)
	}
	if after := readProfileGold(t, db, sess.Userid); after != before {
		t.Fatalf("概率 0 不应加币，余额 %d→%d", before, after)
	}
}

func countLootItems(t *testing.T, db *store.DB, uid int64) int {
	t.Helper()
	var n int
	if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM item WHERE uid=?`, uid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// setupTrophySplitTest 准备拆分测试环境：开总开关、礼包概率 0（确保只能靠保底/奖杯概率触发）、
// 建玩家并记录初始金币。返回服务、DB、会话、初始金币。
func setupTrophySplitTest(t *testing.T) (*Server, *store.DB, *Session, int64) {
	t.Helper()
	srv, db := newRigTestServer(t)
	sess := newProbeSess(srv)
	enableLootBox(t, srv, db)
	setWeatherTestTunable(t, srv, "loot_box_chance", "0")
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.SQL.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mustExec(`INSERT INTO kv(k,v) VALUES('loot_box_defs','[{"id":"split_box","name":"测试礼包","chance":0}]')
		ON CONFLICT(k) DO UPDATE SET v=excluded.v`)
	if err := srv.EnsurePlayer(context.Background(), sess.Userid); err != nil {
		t.Fatal(err)
	}
	return srv, db, sess, readProfileGold(t, db, sess.Userid)
}

// TestLootBoxBlueGuaranteeAlwaysOpens 蓝冠(g4)在通用概率为 0 时仍必开一包（保底默认 1）。
func TestLootBoxBlueGuaranteeAlwaysOpens(t *testing.T) {
	srv, db, sess, before := setupTrophySplitTest(t)
	got := srv.rollLootBox(context.Background(), sess, &RolledFish{Species: "c.blue", Grade: 4})
	if got != "gold" {
		t.Fatalf("蓝冠必保底开一包，实得 %q", got)
	}
	if d := readProfileGold(t, db, sess.Userid) - before; d < lootGoldDefaultLo || d > lootGoldDefaultHi {
		t.Fatalf("保底应加 [%d,%d] 分金币，实得 %d", lootGoldDefaultLo, lootGoldDefaultHi, d)
	}
}

// TestLootBoxBlueGuaranteeCanBeDisabled 蓝冠保底开关 0=关：通用概率 0 时蓝冠也不开。
func TestLootBoxBlueGuaranteeCanBeDisabled(t *testing.T) {
	srv, db, sess, before := setupTrophySplitTest(t)
	setWeatherTestTunable(t, srv, "loot_box_trophy_guarantee", "0")
	got := srv.rollLootBox(context.Background(), sess, &RolledFish{Species: "c.blue", Grade: 4})
	if got != "" {
		t.Fatalf("蓝冠保底关闭且通用概率 0，不应开箱，实得 %q", got)
	}
	if after := readProfileGold(t, db, sess.Userid); after != before {
		t.Fatalf("关闭保底不应加币，余额 %d→%d", before, after)
	}
}

// TestLootBoxTrophyUsesOwnChance 奖杯(g3)走独立概率：概率 1 必开，概率 0 不开，
// 且概率 0 时不叠加通用概率（通用概率已设 0）。
func TestLootBoxTrophyUsesOwnChance(t *testing.T) {
	srv, db, sess, before := setupTrophySplitTest(t)
	// 概率 0：奖杯不开。
	setWeatherTestTunable(t, srv, "loot_box_trophy_chance", "0")
	if got := srv.rollLootBox(context.Background(), sess, &RolledFish{Species: "c.trophy", Grade: 3}); got != "" {
		t.Fatalf("奖杯概率 0 不应开箱，实得 %q", got)
	}
	// 概率 1：奖杯必开。
	setWeatherTestTunable(t, srv, "loot_box_trophy_chance", "1")
	if got := srv.rollLootBox(context.Background(), sess, &RolledFish{Species: "c.trophy", Grade: 3}); got != "gold" {
		t.Fatalf("奖杯概率 1 必开，实得 %q", got)
	}
	if d := readProfileGold(t, db, sess.Userid) - before; d < lootGoldDefaultLo || d > lootGoldDefaultHi {
		t.Fatalf("奖杯命中应加 [%d,%d] 分金币，实得 %d", lootGoldDefaultLo, lootGoldDefaultHi, d)
	}
}

// TestLootBoxTrophyIgnoresBlueGuarantee 奖杯概率与蓝冠保底开关分开：保底关掉不影响奖杯概率。
func TestLootBoxTrophyIgnoresBlueGuarantee(t *testing.T) {
	srv, db, sess, before := setupTrophySplitTest(t)
	setWeatherTestTunable(t, srv, "loot_box_trophy_guarantee", "0")
	setWeatherTestTunable(t, srv, "loot_box_trophy_chance", "1")
	got := srv.rollLootBox(context.Background(), sess, &RolledFish{Species: "c.trophy", Grade: 3})
	if got != "gold" {
		t.Fatalf("蓝冠保底关闭不影响奖杯概率，奖杯概率 1 应开，实得 %q", got)
	}
	if d := readProfileGold(t, db, sess.Userid) - before; d < lootGoldDefaultLo || d > lootGoldDefaultHi {
		t.Fatalf("奖杯命中应加 [%d,%d] 分金币，实得 %d", lootGoldDefaultLo, lootGoldDefaultHi, d)
	}
}

// TestLootBoxTrophyChanceDefault 固化上星概率默认值 0.5 与值域钳制（负→0，超 1→1）。
func TestLootBoxTrophyChanceDefault(t *testing.T) {
	srv, db := newRigTestServer(t)
	if got := srv.lootBoxTrophyChance(); got != 0.5 {
		t.Fatalf("上星概率默认应为 0.5，实得 %v", got)
	}
	setWeatherTestTunable(t, srv, "loot_box_trophy_chance", "-1")
	if got := srv.lootBoxTrophyChance(); got != 0 {
		t.Fatalf("负概率应钳为 0，实得 %v", got)
	}
	setWeatherTestTunable(t, srv, "loot_box_trophy_chance", "3")
	if got := srv.lootBoxTrophyChance(); got != 1 {
		t.Fatalf("超 1 概率应钳为 1，实得 %v", got)
	}
	_ = db
}

// TestLootBoxQualifiedNeverOpens 用户修正：福袋只在上星(3)/蓝冠(4)开。
// 合格(2)及以下即使通用概率为 1 也不开，避免"合格开福袋、上星蓝冠反而没有"。
func TestLootBoxQualifiedNeverOpens(t *testing.T) {
	srv, db, sess, before := setupTrophySplitTest(t)
	// 通用概率拉满、上星概率拉满，排除概率因素，只验证档位闸口。
	setWeatherTestTunable(t, srv, "loot_box_chance", "1")
	setWeatherTestTunable(t, srv, "loot_box_trophy_chance", "1")
	for _, grade := range []int64{0, 1, 2} {
		if got := srv.rollLootBox(context.Background(), sess, &RolledFish{Species: "c.roach", Grade: grade}); got != "" {
			t.Fatalf("grade=%d(合格及以下)不应开福袋，实得 %q", grade, got)
		}
	}
	if after := readProfileGold(t, db, sess.Userid); after != before {
		t.Fatalf("合格及以下不应加币，余额 %d→%d", before, after)
	}
	// 上星(3)与蓝冠(4)应开。
	if got := srv.rollLootBox(context.Background(), sess, &RolledFish{Species: "c.trophy", Grade: 3}); got != "gold" {
		t.Fatalf("上星概率 1 应开福袋，实得 %q", got)
	}
	if got := srv.rollLootBox(context.Background(), sess, &RolledFish{Species: "c.blue", Grade: 4}); got != "gold" {
		t.Fatalf("蓝冠保底应开福袋，实得 %q", got)
	}
}

// TestDrawLootGoldRange 所有抽取值必须落在 [lo,hi] 且为整数分。
func TestDrawLootGoldRange(t *testing.T) {
	cfg := lootGoldConfig{lo: 1, hi: 200, sigma: 30, targetMean: 100}
	for i := 0; i < 5000; i++ {
		v := drawLootGold(cfg)
		if v < cfg.lo || v > cfg.hi {
			t.Fatalf("抽取值 %d 越界 [%d,%d]", v, cfg.lo, cfg.hi)
		}
	}
}

// TestDrawLootGoldMean 大样本下均值应接近校准目标（截断正态，容差 4 分）。
func TestDrawLootGoldMean(t *testing.T) {
	cfg := lootGoldConfig{lo: 1, hi: 200, sigma: 30, targetMean: 100}
	const n = 40000
	var sum int64
	for i := 0; i < n; i++ {
		sum += drawLootGold(cfg)
	}
	mean := float64(sum) / n
	if math.Abs(mean-cfg.targetMean) > 4 {
		t.Fatalf("均值 %.2f 偏离目标 %.0f 超过 4 分", mean, cfg.targetMean)
	}
}

// TestDrawLootGoldSigmaControlsSpread σ 越小越集中：小 σ 的样本标准差应显著小于大 σ。
func TestDrawLootGoldSigmaControlsSpread(t *testing.T) {
	stddev := func(sigma float64) float64 {
		cfg := lootGoldConfig{lo: 1, hi: 200, sigma: sigma, targetMean: 100}
		const n = 20000
		var sum, sumSq int64
		for i := 0; i < n; i++ {
			v := drawLootGold(cfg)
			sum += v
			sumSq += v * v
		}
		m := float64(sum) / n
		return math.Sqrt(float64(sumSq)/n - m*m)
	}
	narrow := stddev(5)
	wide := stddev(60)
	if !(narrow < wide*0.5) {
		t.Fatalf("σ=5 的标准差 %.2f 应显著小于 σ=60 的 %.2f", narrow, wide)
	}
}

// TestLootBoxGoldConfigClamp 后台参数越界时应被钳制到安全范围。
func TestLootBoxGoldConfigClamp(t *testing.T) {
	srv, _ := newRigTestServer(t)
	// 默认参数。
	c := srv.lootBoxGoldConfig()
	if c.lo != lootGoldDefaultLo || c.hi != lootGoldDefaultHi || c.sigma != lootGoldDefaultSigma || c.targetMean != lootGoldDefaultMean {
		t.Fatalf("默认参数不符：%+v", c)
	}
	// hi < lo、lo<1、sigma 非法、mean 越界 → 全部钳制。
	setWeatherTestTunable(t, srv, "loot_box_gold_lo", "-5")
	setWeatherTestTunable(t, srv, "loot_box_gold_hi", "0")
	setWeatherTestTunable(t, srv, "loot_box_gold_sigma", "0")
	setWeatherTestTunable(t, srv, "loot_box_gold_mean", "9999")
	c = srv.lootBoxGoldConfig()
	if c.lo != 1 || c.hi != 1 || c.sigma != lootGoldDefaultSigma || c.targetMean != 1 {
		t.Fatalf("越界参数未按预期钳制：%+v", c)
	}
	// 区间过大钳制。
	setWeatherTestTunable(t, srv, "loot_box_gold_lo", "1")
	setWeatherTestTunable(t, srv, "loot_box_gold_hi", "999999")
	c = srv.lootBoxGoldConfig()
	if c.hi-c.lo > lootGoldMaxSpan {
		t.Fatalf("区间未钳制到上限 %d：%+v", lootGoldMaxSpan, c)
	}
}

// TestLootBoxGoldCustomRange 自定义范围时抽取值落在新的 [lo,hi] 内，均值贴近新目标。
func TestLootBoxGoldCustomRange(t *testing.T) {
	cfg := lootGoldConfig{lo: 50, hi: 150, sigma: 20, targetMean: 120}
	var sum int64
	const n = 20000
	for i := 0; i < n; i++ {
		v := drawLootGold(cfg)
		if v < cfg.lo || v > cfg.hi {
			t.Fatalf("抽取值 %d 越界 [%d,%d]", v, cfg.lo, cfg.hi)
		}
		sum += v
	}
	mean := float64(sum) / n
	if math.Abs(mean-cfg.targetMean) > 5 {
		t.Fatalf("自定义范围均值 %.2f 偏离目标 %.0f 超过 5 分", mean, cfg.targetMean)
	}
}
