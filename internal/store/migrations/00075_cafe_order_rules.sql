-- 咖啡厅悬赏追加：普通/达标位的交单数量矩阵 + 各槽位轮换时间。
--
-- 交单数量（cafe_order_quantity）按「重量档(普通/达标) × 鱼种稀缺(常见/稀有)」四维配置，
-- 仅作用于 7 个普通/达标位；奖杯位与长期位仍是单条大鱼，数量为 1。
--   普通鱼（普通档·常见）   5 条
--   达标鱼（达标档·常见）   3 条
--   稀有普通鱼（普通档·稀有）3 条
--   稀有达标鱼（达标档·稀有）2 条
--
-- 轮换时间（cafe_slot_period）按槽位类别配置，min=max 表示固定周期：
--   长期位 24 小时、奖杯位 12 小时、普通/达标位 1~4 小时（逐槽随机）。
--
-- 候选池表补充 quantity 列，随池行落库，供订单生成直接使用。

CREATE TABLE IF NOT EXISTS cafe_order_quantity (
    qkey TEXT PRIMARY KEY CHECK(qkey IN ('common_normal','common_qualified','rare_normal','rare_qualified')),
    quantity INTEGER NOT NULL CHECK(quantity > 0 AND quantity <= 1000),
    label TEXT NOT NULL DEFAULT ''
);

INSERT OR IGNORE INTO cafe_order_quantity(qkey, quantity, label) VALUES
    ('common_normal',    5, '普通鱼（普通档·常见）'),
    ('common_qualified', 3, '达标鱼（达标档·常见）'),
    ('rare_normal',      3, '稀有普通鱼（普通档·稀有）'),
    ('rare_qualified',   2, '稀有达标鱼（达标档·稀有）');

CREATE TABLE IF NOT EXISTS cafe_slot_period (
    kind TEXT PRIMARY KEY CHECK(kind IN ('trophy','longterm','normal')),
    min_seconds INTEGER NOT NULL CHECK(min_seconds > 0),
    max_seconds INTEGER NOT NULL CHECK(max_seconds >= min_seconds),
    label TEXT NOT NULL DEFAULT ''
);

INSERT OR IGNORE INTO cafe_slot_period(kind, min_seconds, max_seconds, label) VALUES
    ('longterm', 86400, 86400, '长期位 24 小时'),
    ('trophy',   43200, 43200, '奖杯位 12 小时'),
    ('normal',    3600, 14400, '普通/达标位 1~4 小时');

-- 已建库补列：候选池行增加交单数量，旧行默认 1 条。
ALTER TABLE cafe_bounty_pool ADD COLUMN quantity INTEGER NOT NULL DEFAULT 1;

-- 旧池行没有新配置（数量/周期仍为旧值），清空让 gameserver 启动时按新配置自动重建。
-- 候选池是纯派生数据，可安全重建。
DELETE FROM cafe_bounty_pool;
