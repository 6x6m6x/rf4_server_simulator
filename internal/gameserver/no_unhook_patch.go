package gameserver

// 用户补丁：彻底关闭所有中钩否决（脱钩），不限于水底。
//
// 根因：原脱钩有两条独立否决路径，且都不受嘬饵开关 hook_nibble_enabled 控制：
//  1. applyHookBiteQuality 把超钩容量的鱼标为 nibble（仅 hookNibbleEnabled 开启时）；
//  2. hookRetentionFails 在 14/9、14/11 中钩确认时对 solid 鱼再按 HookHoldP 掷骰失败。
//
// 后者即使嘬饵关闭也生效，失败后走 finishNibbleHook 静默放鱼（推 14/15 鱼离场，
// 无脱钩提示），表现为"响几下鱼没了"。旧的"底钓防脱钩补丁"只覆盖沉底族嘬饵，
// 完全没有触及这条保持率否决路径，故作废。
//
// 本补丁在否决机制源头集中硬关闭：hookRejectDisabled 默认 true，
// 使 hookNibbleEnabled() 与 hookRetentionFails() 恒为 false，
// 14/9 与 14/11 中钩确认必然成功，不再静默放鱼。
// 单元测试可临时把 hookRejectDisabled 置为 false 以验证原机制本身完整。
var hookRejectDisabled = true
