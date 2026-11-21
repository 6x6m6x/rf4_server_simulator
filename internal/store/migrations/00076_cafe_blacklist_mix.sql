-- 咖啡厅悬赏追加：按水域的悬赏黑名单 + 可配置槽位占比。
--
-- 悬赏黑名单（cafe_bounty_blacklist）：按 level 排除鱼种，命中的鱼不会进入该水域的候选池。
-- 与静态白名单（随程序打包的 cafe_bounty_whitelist.json，决定 rare/giant）互不影响。
--
-- 槽位占比（cafe_slot_mix）：奖杯/长期/普通三类槽位数量，总和固定为 10。
-- 候选池按此分配 slot，改动后需重建候选池。
--
-- 旧池行的槽位分布仍是缺省 2/1/7，且未应用黑名单过滤，清空让 gameserver 启动时自动重建。

CREATE TABLE IF NOT EXISTS cafe_bounty_blacklist (
    level TEXT NOT NULL,
    fish_key TEXT NOT NULL,
    name_zh TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(level, fish_key)
);

CREATE TABLE IF NOT EXISTS cafe_slot_mix (
    kind TEXT PRIMARY KEY CHECK(kind IN ('trophy','longterm','normal')),
    count INTEGER NOT NULL CHECK(count >= 0 AND count <= 10),
    label TEXT NOT NULL DEFAULT ''
);

INSERT OR IGNORE INTO cafe_slot_mix(kind, count, label) VALUES
    ('trophy',   2, '奖杯位'),
    ('longterm', 1, '长期位'),
    ('normal',   7, '普通/达标位');

-- 白名单数据已固化到静态文件，DB 旧表不再读写，清空避免误导（表结构保留以兼容旧库）。
DELETE FROM cafe_bounty_fish;

-- 旧池行未应用黑名单/新占比，清空让 gameserver 启动时按新配置自动重建。
DELETE FROM cafe_bounty_pool;
