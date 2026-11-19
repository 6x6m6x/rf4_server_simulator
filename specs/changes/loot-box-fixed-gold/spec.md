# 钓获礼包奖励改为固定 1 金币

Status: done

## Problem Statement

钓上鱼入护成功后的"钓获礼包"目前是开箱随机抽物品：按 `kv.loot_box_defs` 的 `rewards` 权重或商店货架随机抽一件商品进背包。玩家和运维希望奖励确定可控——每次开箱固定给 1 金币，不再随机抽取物品，便于本地测试与预期管理。触发条件（总开关、概率、奖杯保底）本身没有问题，不应改动。

## Solution

保持钓获礼包的触发条件完全不变（后台 `loot_box_enabled` 总开关、`loot_box_chance` 概率、奖杯/蓝冠保底），只把"开箱命中后发什么"从随机物品改为固定 1 金币：命中即在事务内写入 `gold +100` 与一条收支流水，刷新客户端档案并聊天播报。随机抽取逻辑完整保留，通过一个集中开关控制，可随时恢复原语义。

## User Stories

1. 作为玩家，我希望钓上鱼开箱后稳定获得 1 金币，而不是随机拿到不确定物品。
2. 作为玩家，我希望金币当场到账并在角色面板立即刷新可见。
3. 作为运维，我希望不改触发节奏——礼包多久开一次仍由原有概率和保底配置决定。
4. 边界：礼包总开关关闭或概率未命中时，既不抽物品也不加金币。
5. 边界：同一尾鱼重复上报 14/5 时金币只加一次，靠既有入护幂等保证。
6. 边界：需要恢复随机开箱时，改一处开关初值重编即可，配置与代码都还在。

## Implementation Decisions

- `internal/gameserver/loot_box.go`：新增包级开关 `disableLootBoxRandomReward = true`（生产默认 true=固定金币）与常量 `lootBoxFixedGoldUnits = 100`（项目经济口径百分位，100=1 金）。
- `internal/gameserver/loot_box.go`：`grantLootBox` 按开关分流——固定金币走 `grantLootBoxFixedGold`，关闭时回退 `grantLootBoxRandomItem`（原 `grantLootBox` 随机抽取实现，完整保留）。
- `internal/gameserver/loot_box.go`：`grantLootBoxFixedGold` 用 `beginEconomyTx` + `applyWalletTx` 原子写 `gold +100` 与 `kind=loot_box` 流水，提交后 `sess.Push2_1()` 刷新档案（2/1 的 f8 携带 gold），再 `pushSystemChat` 播报「获得：金币 ×1」，返回 `"gold"` 作为命中标记。
- `internal/gameserver/econ.go`：`fishingKeep`（14/5）的调用点与异步 `scheduleLootBox` 不改；触发判定 `rollLootBox` 的总开关、概率与保底逻辑不改。
- 数据与迁移：无表结构、无迁移变更；写现有 `profile.gold` 与 `txn` 流水表，复用既有幂等键。
- 兼容：无客户端协议、字段或命令变化，无需新增 opcode；`kv.loot_box_defs` 的 `rewards` / `carrier_item` 在固定金币模式下保留但不读取。回滚方式为把 `disableLootBoxRandomReward` 初值改为 false 重新编译。

## Testing Decisions

- 纯逻辑/存储测试：`internal/gameserver/loot_box_test.go` 新增 `TestLootBoxFixedGoldDefault`，钉住生产默认关闭随机奖励——命中后返回 `gold`、`gold` 增加 100、不落任何物品行、写一条 `loot_box` 金币流水。
- 行为测试：`TestLootBoxFixedGoldRespectsChance` 验证概率为 0 时既不开箱也不加币（触发条件不变）。
- 回归测试：原有 5 个依赖随机物品的测试通过 `enableLootBoxRandomRewardForTest` 临时打开随机机制，继续验证权重抽取、商店兜底、不可物化类别过滤、券逐行推送与 14/5 重放幂等；测试结束自动恢复默认固定金币。
- 回归门禁：`go test ./... -count=1`、`go vet ./...`、`go build ./...`、doccheck 全绿。
- 期望值来自明确字面量（gold 增量 100、返回标记 `gold`），不用被测代码自算。
- 回滚后变红：`TestLootBoxFixedGoldDefault` 在开关初值改 false 后会变红，提示当前处于随机物品语义状态。

## Out of Scope

- 不改变礼包触发时机、总开关、概率、奖杯/蓝冠保底逻辑。
- 不新增后台参数或管理页面入口；奖励内容由编译期开关控制。
- 不删除原随机抽取与商店兜底代码，保持可回滚。
- 不改变金币单位口径与会员定价（沿用百分位，100=1 金）。
- 不调整 `kv.loot_box_defs` 的配置格式，旧配置在回滚后仍可用。

## Further Notes

- `[事实]` 项目经济口径中 `profile.gold` 为金币百分位：`world.go` 注释 `45 = 0.45 金`，会员 1 天 `PriceGold=100`，故 1 金币 = 100。
- `[事实]` 发奖幂等的唯一归属是入护结算回执（`settlement-and-idempotency.md`）；礼包异步挂在 14/5 成功之后，重复 14/5 不会重复触发。
- `[事实]` 档案协议 2/1 的 f8 字段携带 gold（`profilebuild.go`），发金币后须 `Push2_1` 客户端才可见。
- `[推断]` 以单一开关集中切换奖励内容，比逐处改抽奖代码更安全：随机机制保持可测、可一键恢复，降低本地测试服与官服语义分叉的维护成本。
