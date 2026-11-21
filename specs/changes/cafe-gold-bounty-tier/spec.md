# 咖啡厅赏金金币化与梯度重划

Status: done

## 需求

将咖啡厅任务奖励由银币改为金币；每个地图按鱼种权重重新划分赏金梯度。
初版为三档；v4 迭代采纳“稀有鱼蓝冠 ≠ 常见鱼蓝冠”的反馈，改为**二维矩阵六档**。

## 口径

- 金币以“分”存储，100 分 = 1 金币（与 `loot_box_gold_*` 一致）。
- “权重”= 鱼种在该地图的捕获权重 `loc_fish.catches`（高=常见，低=稀有）；
  `fish_record_weights.json` 是达标重量，非概率权重，不采用。
- 档位 = `gradeFromLines(qualified/trophy/blue, minWeightG)`：1 普通 / 2 达标 / 3 奖杯 / 4 蓝冠。
- 稀有鱼种 = 本图捕获权重排名落在最稀疏 30%，即 `rarity >= 0.7`。

## 算法（`internal/gameserver/cafe_gold_reward.go`）

1. `CafeRarityMap`：按地图鱼种捕获权重升序排秩，稀有度 = 1 - 秩/(n-1)，同权重取并列平均秩。
2. `CafeGoldBounty(rarity, grade, weightG, baseG)`：**重量档 × 鱼种稀缺** 二维矩阵，6 格独立区间（金币分）：

   | 重量档 ＼ 鱼种稀缺 | 常见（rarity<0.7） | 稀有（rarity≥0.7） |
   |---|---|---|
   | 普通/达标（grade 1-2） | 50~500（0.5~5 金） | 300~800（3~8 金） |
   | 奖杯（grade 3） | 500~1500（5~15 金） | 2500~4000（25~40 金） |
   | 蓝冠（grade 4） | 1200~2500（12~25 金） | 3500~5000（35~50 金） |

   核心：常见鱼蓝冠与稀有鱼蓝冠价值彻底分离，不再共用区间。
3. 区间内**双因子插值**：`t = clamp(稀有度因子×0.65 + 重量比因子×0.35)`，`赏金 = lo + round(t×(hi-lo))`。
   - 稀有度因子：在本格所属 rarity 段内归一化（常见段 rarity/0.7，稀有段 (rarity-0.7)/0.3）。
   - 重量比因子：`(要求重量/档基准重量 - 1)/0.5`，clamp 到 [0,1]；档基准由 `cafeBandBaseG`
     按档位取达标线/奖杯线/蓝冠线。基准缺失时该因子为 0（退化为单稀有度因子）。
   - 效果：稀有鱼 + 超重个体顶到该档上限（稀有蓝冠 50 金）；常见鱼 + 刚过线取下限。
4. `calibratedCafeTemplates` 用上述结果替换原观测价格 `cafeReward`/`cafeScaledReward`，
   `PricingVersion = CafeGoldPricingVersion (4)`（v3 三档旧单失效重建）；原模板价格字段保留作溯源，不参与结算。

## 结算（`internal/store/repos_cafe.go`）

`SubmitCafeOrder` 从 `profile.silver` 改为 `profile.gold`，txn 由 `silver_delta` 改为 `gold_delta`；
溢出校验、幂等回执、重复交付拦截保持不变。

## 可视化（`cmd/cafe-bounty-report`）

`BuildCafeBountyReport` 复用运行时 `cafeTemplates` 生成 17 水域 × 10 槽位快照，
导出自包含 `output/咖啡厅悬赏金币梯度.html`：六档图例/筛选/统计，逐单列出鱼种中文名、
稀有鱼标记、档位、要求重量、重量比因子、捕获权重、稀有度、梯度、悬赏，支持搜索与梯度筛选。

## 验证

- `go test ./internal/gameserver ./internal/store -run TestCafe`：通过（六档边界、价值分离、双因子单调性单测）。
- 全量 `go test ./...`：通过（doccheck 需 spec 含 Status 行，已补齐）。
- 导出数据校验：170 槽位赏金全部落在规定区间，0 越界；内嵌 JS 语法通过 `node --check`。
- 数据观察：当前订单快照中蓝冠单 4 笔均为常见鱼（稀有鱼最高到奖杯档），故稀有蓝冠档暂无样本；
  分档规则已就位并经单测验证，出现稀有鱼蓝冠单时自动落入 35~50 金区间，未为凑样本改动订单逻辑。
- note：20/4 `hMarketSellOffer` 是 `hCafeSubmit` 别名，相关测试同步改为金币断言。
