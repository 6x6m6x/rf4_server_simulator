# 变更：钓获礼包金币奖励改为截断正态随机

Status: done

## 背景

上一版把钓获礼包奖励固定为 1 金币（`gold+100`）。用户要求改为随机金额：
默认 1~200 分（0.01~2 金）、集中度 σ=30，且范围、集中度、期望均可后台调整，
长期均值仍精确约 100 分。用户给出了 Python 原型（截断正态 + 二分校准 μ +
累积权重二分查找）。

## 约束路径（决定）

- ✅ 金额取自**截断正态分布**：权重 `w(x)=exp(-(x-μ)²/(2σ²))`，x 遍历 `[lo,hi]`
  每个整数分；80 次二分校准 μ 使加权期望≈目标期望 `loot_box_gold_mean`；
  再按累积分布 `CUM` 采样 `r=random×CUM末`，二分定位 x。与用户 Python 原型等价。
- ✅ 四个后台可调参数（30 秒缓存）：
  - `loot_box_gold_lo` 下限（分，默认 1，最小 1）；
  - `loot_box_gold_hi` 上限（分，默认 200）；
  - `loot_box_gold_sigma` 集中度 σ（默认 30，越小越集中）；
  - `loot_box_gold_mean` 校准目标期望（分，默认 100）。
- ✅ 边界钳制：lo≥1、hi≥lo、区间≤100000 分、σ≥1e-3（非法值回退默认）、
  mean 落在 `[lo,hi]`（NaN/Inf 回退区间中点）；单点区间 `[lo,lo]` 直接返回 lo。
  - ❌ 裸正态不校准 μ → 截尾偏移均值，金额期望与后台目标不符。
  - ❌ σ≤0 / 区间无上限 → 除零、病态分布、超大数组。
- ✅ 发奖仍走事务 `applyWalletTx` 写 `gold +N` + `kind=loot_box` 流水 → commit →
  `Push2_1` 刷档案 → 聊天播报「获得：金币 X.XX（N 分）」。幂等/容错不变。
- ✅ 触发条件不变：总开关 → 蓝冠保底 → 奖杯独立概率 → 普通鱼通用概率。
  本次只改"开出多少钱"，不改"是否开出"。
- ✅ 随机抽物品逻辑仍完整保留：`disableLootBoxRandomReward=false` 可回退。

## 实现落点

- `internal/gameserver/loot_box.go`：新增 `lootGoldConfig` / `lootBoxGoldConfig()` /
  `drawLootGold()`；`grantLootBoxFixedGold` 更名 `grantLootBoxGold` 并改用随机金额。
- `internal/gameserver/loot_box_test.go`：新增 `TestDrawLootGoldRange`（范围）、
  `TestDrawLootGoldMean`（4 万样本均值≈目标）、`TestDrawLootGoldSigmaControlsSpread`
  （σ 控集中度）、`TestLootBoxGoldConfigClamp`（边界钳制）、`TestLootBoxGoldCustomRange`
  （自定义范围）；原固定金额断言改为范围断言。
- `internal/admin/web/index.html`：登记 4 个参数到「收益与通知 → 钓获礼包」分组。
- `docs/loot-box.md` 重写、`specs/capabilities/economy-and-rewards.md` 第 5 条回填。

## 验证

- 专项测试（含 4 万样本均值、σ 集中度对比）全绿。
- 未重新编译 exe（用户要求本轮只改代码、暂不编译）。

## 回滚

把 `grantLootBoxGold` 里 `amount := drawLootGold(...)` 改回 `amount := lootBoxFixedGoldUnits`
（需恢复该常量=100）即可回到固定 1 金币；或把 `disableLootBoxRandomReward` 初值改 false
回到随机抽物品。
