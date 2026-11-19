package gameserver

// 用户补丁 v1：解除铜湖(level_018_moskitnoe)的全部钓具限制。
//
// 原规则表 assets/places_restrictions.json 对铜湖登记了三条限制：
//   - category 3 / subtype 11：carp 鲤竿（赛杆）全禁
//   - category 3 / subtype 5 ：match 竞技竿档位≥4 禁
//   - category 6 / subtype 1 ：spin 纺车轮 size≥6 禁（6000 以上线杯）
//
// copperRestrictionDisabled 默认 true：restrictionsForLevel 对铜湖返回 nil，
// 17/7 取竿、14/1 抛竿准备、14/2 抛竿三个服务端入口全部放行。
// 其它地图规则不变。单元测试可临时置 false 验证原机制完整。
//
// 注意：客户端 ebjhdhighdj 静态注册表另有本地判定（红字"禁止在当前水体
// 上使用"），本补丁不触及客户端。实测客户端本地仍会拦截抛竿请求；
// 彻底解除需改客户端，属另一专题。
var copperRestrictionDisabled = true

const copperLakeLevelKey = "level_018_moskitnoe"
