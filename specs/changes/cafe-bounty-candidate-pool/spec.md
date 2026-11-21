# 咖啡厅悬赏任务：预生成候选池 + 后台管理

Status: done

## 需求

撇开 `cafe_reward_templates.json`，按各可钓图特色重建随机悬赏候选池，鼓励玩家追求高额悬赏的巨物与稀有鱼；
并在后台管理加入可视化维护界面。

- 鱼种清单取自 `loc_fish`（可钓图），中文名取自 `item_catalog`，重量要求取该鱼种档位线。
- 10 槽位分配：**2 奖杯位 + 1 长期位 + 7 普通/达标位**。
- 长期位放稀有奖杯/蓝冠：特色稀有鱼（七鳃鳗 3 种、真柱白鲑、钓𩽾𩾌、多指鞭冠𩽾𩾌）与巨物白名单
  （姥鲨、剑鱼、各类大型鲟鱼、欧洲鳇、波罗的海鲟鱼、欧洲六须鲇等）。
- 赏金区间（金币）：普通/达标 常见 0.5~2.5 / 稀有 2.5~5；奖杯 常见 8~18 / 稀有 18~28；
  蓝冠/长期 常见 18~28 / 稀有 28~48。巨物按蓝冠线分重量级补贴，封顶 100 金。

## 用户确认的口径

1. 无稀有/巨物白名单的 6 张图，长期位**退化为本图 blue 线以上的普通鱼**（方案 a）。
2. 七鳃鳗只补现有 3 种（里海/乌克兰/亚马河），其余 4 种不采用。
3. 巨物补贴按蓝冠线分重量级，采纳 g1≥1吨 / g2 10万g~1吨 / g3<10万g。
4. 候选池**预生成固定**，落库后按现实周期轮换。
5. 后台管理新增咖啡厅订单管理页。

## 追加口径（2026-11-20，迁移 00072）

- **交单数量**：7 个普通/达标位按「重量档 × 稀缺」四维配置交单条数——
  普通鱼 5 / 达标鱼 3 / 稀有普通鱼 3 / 稀有达标鱼 2；奖杯位、长期位恒为 1 条。
  普通档=grade1（重量取达标线一半）、达标档=grade2（重量取达标线）；
  后台可改，生成器按 `cafe_order_quantity` 落库到池行 `quantity`。
- **轮换时间**：长期位固定 24 小时、奖杯位固定 12 小时、普通/达标位逐槽 1~4 小时；
  后台 `cafe_slot_period` 可改（min=max 表示固定周期）。

## 追加口径（2026-11-21，迁移 00073）

- **白名单移到静态文件**：rare/giant 分类数据从 DB 表 `cafe_bounty_fish` 固化到
  `internal/gameserver/assets/cafe_bounty_whitelist.json`（go:embed），后台不再编辑白名单；
  稀有度强制、巨物补贴、长期位优先等逻辑不变。迁移清空旧 DB 白名单表数据。
- **悬赏黑名单（按水域）**：加入的鱼种不进入该水域候选池。存 `cafe_bounty_blacklist(level, fish_key, name_zh)`，
  生成时按 `level` 过滤 `usable`。后台选水域 + 输入中文名/fish_key 即时模糊匹配添加（数据源 `loc_fish` + `item_catalog`）。
- **槽位占比可配**：三类槽位数量存 `cafe_slot_mix(kind, count)`，总和固定 10；
  `cafeSlotKinds` 按奖杯→长期→普通顺序展开。`cafeTemplatesFromPool` 与可视化按实际槽位集合遍历，不再写死 10。
  缺省 2/1/7 与旧分布一致。

## 数据模型（迁移 00071 / 00072 / 00073）

- `cafe_bounty_band`：六档金币范围（key = normal/trophy/blue × common/rare）。
- `cafe_bounty_fish`：旧白名单表，数据已迁到静态文件，迁移 00073 清空，表结构保留兼容。
- `cafe_giant_subsidy`：巨物按 `min_blue_g` 分级的补贴区间。
- `cafe_bounty_pool`：预生成候选（level/slot/variant/鱼种/重量/档位/稀有/巨物/赏金/补贴/周期相位/交单数量）。
- `cafe_order_quantity`（00072）：四类交单数量（普通/达标 × 常见/稀有）。
- `cafe_slot_period`（00072）：三类槽位轮换周期（秒，min=max 固定）。
- `cafe_slot_mix`（00073）：三类槽位数量，总和 10。
- `cafe_bounty_blacklist`（00073）：按水域的悬赏黑名单。
- 上述表登记在 `store.RuntimeTables`（运营数据，非玩家存档）。

## 生成算法（`internal/gameserver/cafe_bounty_pool.go`）

- `CafeBountyLevels`：从 `loc_alias` ∩ `loc_fish` 取全部 18 个可钓 level（撇开旧 JSON 的 17 个）。
- `rarityFromCatches`：按 `catches` 排名，1=最稀有，同权重并列平均秩；`rarity>=0.7` 为稀有鱼。
- 每槽位 4 条候选（variant），周期取后台 `cafe_slot_period`：长期 24h、奖杯 12h、普通逐槽 1~4h，相位固定写入池。
- 重量取值：普通位=达标线(grade1 取一半/grade2 取达标线)、奖杯位=奖杯线(grade3)、长期位=蓝冠线(grade4)。
- 普通位交单数量取 `cafe_order_quantity`：普通鱼/达标鱼/稀有普通/稀有达标=5/3/3/2，写入池行并透传到订单模板。
- 白名单鱼强制稀有；巨物长期位叠加 `giantSubsidyFor` 补贴（按蓝冠线落档、稀有度插值、至少中档），
  基础赏金+补贴封顶 10000 分。
- 无白名单图：`longPool` 回退为本图蓝冠线最高的普通鱼。
- `cafeTemplatesFromPool` 按 `cycle % len(variants)` 选候选，同一周期稳定；`cafeTemplates` 优先读池，
  池空回退旧 `calibratedCafeTemplates`。`StartAdminSubscriber` 启动时池空则自动生成一次。

## 结算 / 协议

沿用现有 `SubmitCafeOrder`（扣鱼 + 金币 + txn.gold_delta），不改结算链路。
候选池订单定价版本 `cafePoolPricingVersion=5`，与旧 v4 订单隔离，旧单自然过期后切换。

## 后台管理（`internal/admin`）

- 新页 `view-cafe`（侧栏「咖啡厅订单」）：金币范围、巨物补贴、**交单数量**、**轮换时间**、
  **槽位占比**、**按水域黑名单**（中文名即时搜索）、候选池预览（含需交条数）。白名单不再在后台编辑。
- API（均 JWT 鉴权 + 审计）：
  `GET /api/cafe/config`、`POST /api/cafe/band`、`GET /api/cafe/fish/search`、
  `POST /api/cafe/blacklist/add`、`POST /api/cafe/blacklist/remove`、`POST /api/cafe/slot-mix`、
  `POST /api/cafe/subsidy`、`POST /api/cafe/quantity`、`POST /api/cafe/period`、
  `GET /api/cafe/pool`、`POST /api/cafe/pool/regenerate`。
- 写操作后 `QueuePush({"op":"reload_cafe_config"})`；gameserver 消费后重建候选池（约 2 秒）。
- 前端单文件 SPA（内嵌 embed），复用现有 `api()/showModal()/toast()` 惯例。

## 可视化（`cmd/cafe-bounty-report`）

命令自包含：Migrate + 池空则重建；`BuildCafeBountyReport` 优先从池行构建，展示巨物/稀有标记与补贴。
导出 `output/咖啡厅悬赏金币梯度.html`（18 图 ×10 槽位）。

## 验证

- 新增单测 `cafe_bounty_pool_test.go`：生成、槽位分布、补贴分级与封顶、周期稳定性、区间插值。
- 全量 `go test ./...`：通过（表归属测试需新表登记 RuntimeTables，已补）。
- 端到端（真实库副本）：18 水域 × 40 候选 = 720 行，0 越界，最高 88 金；
  无白名单图（梅德韦杰湖）长期位回退黑鲩（蓝冠常见档）；姥鲨 g1 补贴 43.6 金。
- 报告校验：180 槽位六档落界、巨物补贴生效、0 超 100 金。

## 运维注意

- 后台改配置**只写运行库 + 排队重载**，不直接改 gameserver 内存；需 gameserver 在线消费队列才生效。
- 候选池是运行时数据，随游戏库备份/迁移走；`seed` 的 `StaticTables` 不含这些表，不会被覆盖。
- **未部署测试服**（按约定由用户自行替换/迁移）。
