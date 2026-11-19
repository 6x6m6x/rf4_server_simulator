# 钓具助手全服总开关

Status: done

<!-- 验证：实现与自动化门禁已完成（2026-11-16）；客户端实机验证待回归。 -->

## Problem Statement

钓具助手（满红不爆）原本只有玩家侧开关：玩家自己在聊天里发"开/关"。服主没有全服级别的手段。

一旦这个机制出现争议（比如满红不爆让某些鱼变得打不坏、影响装备消耗节奏），服主只有两条路：要么改代码重新发版，要么逐个联系玩家让他们自己关。前者对一个已经在运营的私服来说成本过高，后者在玩家不在线时根本执行不了。

同时，玩家侧的开关保存在玩家库里，服主即使想"全服统一按未开启结算"，也无法让已经开启的玩家立即失效——护栏逻辑分散在断件判定和渔轮计账两处，两处都要判。

## Solution

后台新增全服总开关 `gear_assistant_enabled`（默认开启）。关闭后：钓具助手从聊天玩家列表里隐藏；玩家再给助手发消息只收到一条停用说明；已经开启的玩家立即按"未开启"结算——断件保护失效、锁轮过载照常计磨损。

玩家各自保存的开关不删不改。重新开启总开关后，每个玩家按各自原来的设置继续生效。

## User Stories

1. 作为服主，我希望一键关闭全服的钓具助手，以便在机制出问题时立刻止损，而不用发版。
2. 作为服主，我希望关闭后不改动玩家自己保存的设置，以便重新开启时不必让每个玩家重新设置一次。
3. 作为服主，我希望关闭后已经开启的玩家也立即失效，以便不会出现"名单里没有助手但保护还在生效"的半吊子状态。
4. 作为玩家，我希望助手停用时收到明确的说明，而不是消息石沉大海。
5. 边界：停用期间玩家给助手发任何指令都不改自己的开关；查鱼助手不受影响；重新开启总开关后按原设置恢复。

## Implementation Decisions

- `internal/gameserver`：新增 tunable `gear_assistant_enabled`，缺省 `1`。`gearAssistantEnabled()` 走 `tunableS`，沿用现有 30 秒热更缓存与 `reload_tunables` 推送。
- `internal/gameserver/gear_assistant.go`：总开关关闭时，`receiveGearAssistant` 在进入两级菜单路由**之前**先判总开关，命中则只回一条停用说明邮件并返回，不读取也不写回玩家的 `overload-protection` 键。判定放在菜单路由之前，才能覆盖"玩家已停在二级菜单里"的会话——否则停用期间仍能改自己的开关。
- `internal/gameserver/gear_damage.go`（17/9 断件）：保护判定叠加 `&& s.gearAssistantEnabled()`。这是满红不爆护栏在 v0.1.9 的**唯一落点**——上游已重构渔轮磨损模型（锁轮过载烧毁改为受力磨损速率），`reel_fight_wear.go` 不再读取玩家侧开关，故无需第二处改动。
- `internal/gameserver/fish_query_contact.go`（24/4 成员列表）：总开关关闭时不把钓具助手放进成员数组。顺带把玩家 UID 收集到独立切片再排序——原先 `sort.Slice(ids[2:], ...)` 依赖两个虚拟联系人恒定占据前两位，联系人数量可变后这个下标不再成立。
- `internal/admin`：游戏参数页「玩法与钓组 → 助手开关」分组新增开关按钮；`handleTunableSet` 对该键只接受 `0/1`。
- 协议：无响应形状变更。24/4 仍是普通 u32 数组（`KindU32Arr`），24/19 仍是 bytes 包装的 2002；19/1 仍是 null 应答。客户端版本基线 `4.0.25034`。依据见 `docs/debugging-workflow.md` 的「已确认的协议事实」。
- 数据与迁移：只写现有 `tunables` 表与现有 `kv` 键，无表结构变更、无迁移、无旧库升级需求。
- 兼容与回滚：把 `gear_assistant_enabled` 置回 `1` 即恢复原行为；玩家侧 `kv` 键不受影响，无需清理。

## Testing Decisions

- 行为（gameserver 包，真实 SQLite + 真实分发器）：总开关关闭后，24/4 成员数组里不再出现钓具助手 UID；玩家给助手发"关"后，`overload-protection` 键仍是开启态；停用期间收到的邮件正文包含停用说明。
- 行为（断件链路）：玩家先开启助手并装配可断件钓组，17/9 上报在总开关开启时被取消破坏、总开关关闭后按正常规则断件（`ComponentBroken` 为真）。
- 存储（admin 包）：管理端对 `gear_assistant_enabled` 拒绝非 `0/1` 的值；接受 `0` 后 `tunables` 表落库值为 `"0"`。
- 默认值：未配置该 tunable 时按开启处理（`gearAssistantEnabled()` 为真）。
- 门禁：完整 `go test ./... -count=1`、`go vet ./...`、`go build ./...` 全部通过；`go run ./tools/doccheck` 无 issue。
- 回滚后变红：`TestGearAssistantServerSwitchHidesContactAndKeepsSetting`、`TestGearAssistantServerSwitchDisablesProtection`、`TestAdminAPI`。
- 实机待验收：后台关闭总开关后，客户端玩家列表里助手是否即时消失；已开启玩家的下一条 14/8 是否立即开始计锁轮过载；停用期间发消息是否收到说明。

验证结果：`go build ./...`、`go vet ./...` 通过；`go test ./... -count=1 -timeout=600s` 全部通过（gameserver 202.106 秒、admin 15.279 秒）；`go run ./tools/doccheck` reports docs=27 specs=92 links=38 refs=102 issues=0。新增的 2 个 gameserver 测试与 admin API 断言均通过。本轮未启动真实游戏、未部署；客户端侧表现仍待实机回归。

## Out of Scope

- 不改客户端协议、响应形状或 args code。
- 不改玩家侧开关的语义与存储位置，不清理玩家已保存的设置。
- 不影响查鱼助手、嘬饵机制等其他虚拟联系人与后台开关。
- 不新增后台界面的其他分组或调整既有开关。
- 不做部署或发布。

## Further Notes

- `[事实]` 24/4 客户端返回类型为 `int[]`，必须发普通 u32 数组；24/19 资料卡为 bytes 包装的 2002（`docs/debugging-workflow.md`「已确认的协议事实」）。
- `[事实]` v0.1.9 起满红不爆的护栏落点只有一处：`gear_damage.go` 的 `breakLineOrRodWithLineOut`（17/9 断件）。上游重构渔轮磨损模型后，`reel_fight_wear.go` 的 `recordFightReelLoad` 已不再读取玩家侧开关。
- `[事实]` tunables 缓存窗口 30 秒；后台保存后经 DB 队列投递 `reload_tunables`，gameserver 以 2 秒间隔轮询该队列并清空缓存。
- `[事实]` 原 `24/4` 实现用 `sort.Slice(ids[2:], ...)` 排序玩家段，该写法假定前两位恒为两个虚拟联系人。
- `[事实]` 上游 v0.1.9 的钓具助手是两级菜单（一级选功能、二级开关），单字「开/关/状态」保留为满红不爆的快捷别名。总开关判定放在菜单路由之前，覆盖停在二级的会话。
- `[推断]` 关闭总开关后，已开启玩家若正在搏鱼，受力磨损速率立即恢复计费（不补记保护期间的损耗，与开关本身的既有语义一致）。
- `[假设]` 停用期间收到的说明邮件足以让玩家理解原因；未验证是否需要额外的客户端提示。
