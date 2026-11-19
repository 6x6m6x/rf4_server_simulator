# 变更：钓获礼包——奖杯概率与蓝冠保底分开

Status: done

## 背景

`loot_box.go` 原保底逻辑用 `catchGrade >= 3` 把奖杯(3)和蓝冠(4)合并处理，两者都
"必定开一包"。用户要求把两者拆开：奖杯改为可调整的独立概率，蓝冠保持原保底机制。

## 约束路径（决定）

- ✅ 蓝冠(4)：受 `loot_box_trophy_guarantee` 控制，`1`=必开、`0`=关、默认 `1`；
  判定条件为 `grade >= 4`（覆盖蓝冠及以上），命中后发奖并 return。
- ✅ 奖杯/上星(3)：受新增 `loot_box_trophy_chance` 控制（后台可调，默认 `0.5`，
  `0`=关、`1`=必开，负值钳 0、超 1 钳 1），判定条件为 `grade == 3`。
- ✅ 两条规则互斥且独立：奖杯不再走蓝冠保底；蓝冠保底开关关掉不影响奖杯概率。
- ✅ 用户修正（2026-11-17）：福袋只在上星(3)/蓝冠(4)开；合格(2)及以下直接 return，
  不再落到通用概率 `loot_box_chance` 分支。原实现让合格鱼经通用概率开箱，
  导致"合格开福袋、上星蓝冠反而没有"。上星默认概率从 0.05 改为 0.5。
- ✅ 奖杯分支"命中即开、未命中直接 return"，**不 fall through 到普通鱼通用概率**
  （`loot_box_chance`）。否则奖杯概率调 0 时仍会经通用概率开箱，违背"概率可调整"的意图。
  - ❌ 不要把奖杯概率当成通用概率的"额外一次掷骰"叠加 → 概率值失去精确语义。
- ✅ 一次钓获仍最多开一包：蓝冠保底、奖杯概率、通用概率按序判定，命中即停。
- ✅ 固定 1 金币发奖（`disableLootBoxRandomReward`、`grantLootBoxFixedGold`）不变，
  拆分只改"是否触发"，不改"触发后发什么"。
- ✅ 后台登记：`loot_box_trophy_guarantee` 纳入 0/1 值域白名单；新增
  `loot_box_trophy_chance`（概率数值，不进 0/1 白名单）。

## 实现落点

- `internal/gameserver/loot_box.go`：`rollLootBox` 拆分保底分支；新增
  `lootBoxTrophyChance()` 取值函数。
- `internal/gameserver/loot_box_test.go`：新增 5 个测试覆盖蓝冠必保底 / 蓝冠保底可关 /
  奖杯独立概率 0 与 1 / 奖杯与蓝冠保底互不影响 / 默认值与值域钳制。
- `internal/admin/web/index.html`、`internal/admin/api.go`：后台参数登记与白名单。
- `docs/loot-box.md`、`specs/capabilities/economy-and-rewards.md`：契约回填。

## 回滚

把 `rollLootBox` 的蓝冠/奖杯两条分支合并回 `grade >= 3` 保底即可；后台隐藏
`loot_box_trophy_chance` 参数。固定金币发奖逻辑不受影响。
