-- 咖啡厅悬赏任务：可后台维护的金币梯度配置 + 预生成候选池。
--
-- 背景：赏金梯度从编译期常量/JSON 迁到运行库，便于后台管理页查看、添加、修改，
-- 并在 gameserver 侧通过 admin_queue 的 reload_cafe_config 指令热重载。
--
-- 三张配置表 + 一张候选池表：
--   cafe_bounty_band      六档金币范围（普通/达标、奖杯、蓝冠 × 常见/稀有）
--   cafe_bounty_fish      悬赏鱼白名单（rare 稀有鱼种 / giant 巨物）
--   cafe_giant_subsidy    巨物按蓝冠线分重量级的补贴区间
--   cafe_bounty_pool      预生成固定候选池（每图每槽位多条候选，按周期轮换）
--
-- 金额单位均为金币分（100 分 = 1 金）。默认值来自 2026-11-20 需求口径。

CREATE TABLE IF NOT EXISTS cafe_bounty_band (
    band_key TEXT PRIMARY KEY,
    lo_cents INTEGER NOT NULL CHECK(lo_cents >= 0),
    hi_cents INTEGER NOT NULL CHECK(hi_cents >= lo_cents),
    label TEXT NOT NULL DEFAULT ''
);

INSERT OR IGNORE INTO cafe_bounty_band(band_key, lo_cents, hi_cents, label) VALUES
    ('normal_common',   50,  250, '普通/达标·常见 0.5~2.5 金'),
    ('normal_rare',    250,  500, '普通/达标·稀有 2.5~5 金'),
    ('trophy_common',  800, 1800, '奖杯·常见 8~18 金'),
    ('trophy_rare',   1800, 2800, '奖杯·稀有 18~28 金'),
    ('blue_common',   1800, 2800, '蓝冠·常见 18~28 金'),
    ('blue_rare',     2800, 4800, '蓝冠·稀有 28~48 金');

CREATE TABLE IF NOT EXISTS cafe_bounty_fish (
    fish_key TEXT PRIMARY KEY,
    category TEXT NOT NULL CHECK(category IN ('rare','giant')),
    name_zh TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT ''
);

-- 稀有鱼：3 种现有七鳃鳗 + 真柱白鲑 + 钓鮟鱇 + 多指鞭冠鮟鱇。
INSERT OR IGNORE INTO cafe_bounty_fish(fish_key, category, name_zh, note) VALUES
    ('c_lamprey', 'rare', '里海七鳃鳗', '阿赫图巴河'),
    ('u_lamprey', 'rare', '乌克兰七鳃鳗', '北顿涅茨河'),
    ('fe_lamprey','rare', '七鳃鳗', '亚马河'),
    ('valyok',    'rare', '真柱白鲑', '亚马河 / 下通古斯卡河'),
    ('lophius_p', 'rare', '钓鮟鱇', '挪威海'),
    ('himant_g',  'rare', '多指鞭冠鮟鱇', '挪威海');

-- 巨物白名单：姥鲨、剑鱼、各类鲟鱼、欧洲鳇、达氏鳇、波罗的海鲟鱼、欧洲六须鲇。
INSERT OR IGNORE INTO cafe_bounty_fish(fish_key, category, name_zh, note) VALUES
    ('cetorhinus_m','giant','姥鲨','挪威海'),
    ('x_gladius',   'giant','剑鱼','挪威海'),
    ('r_sturgeon',  'giant','俄罗斯鲟','阿赫图巴河 / 北顿涅茨河'),
    ('osetr_balt',  'giant','波罗的海鲟鱼','拉多加湖 / 拉多加湖群岛'),
    ('osetr_ship',  'giant','裸腹鲟','阿赫图巴河'),
    ('st_sturgeon', 'giant','闪光鲟','阿赫图巴河'),
    ('p_sturgeon',  'giant','波斯鲟','阿赫图巴河'),
    ('zs_sturgeon', 'giant','东西伯利亚鲟','下通古斯卡河'),
    ('sterlet',     'giant','小体鲟','阿赫图巴河 / 白河'),
    ('syb_sterlet', 'giant','西伯利亚小体鲟','下通古斯卡河'),
    ('beluga',      'giant','欧洲鳇','阿赫图巴河'),
    ('kaluga',      'giant','达氏鳇','亚马河'),
    ('w_catfish',   'giant','欧洲六须鲇','多图');

CREATE TABLE IF NOT EXISTS cafe_giant_subsidy (
    grp TEXT PRIMARY KEY,
    min_blue_g INTEGER NOT NULL,
    lo_cents INTEGER NOT NULL CHECK(lo_cents >= 0),
    hi_cents INTEGER NOT NULL CHECK(hi_cents >= lo_cents),
    label TEXT NOT NULL DEFAULT ''
);

-- 按蓝冠线 blue_trophy_g 分重量级；基础赏金 + 补贴后统一封顶 100 金（10000 分）。
INSERT OR IGNORE INTO cafe_giant_subsidy(grp, min_blue_g, lo_cents, hi_cents, label) VALUES
    ('g1', 1000000, 4000, 5200, '≥1 吨（姥鲨/欧洲鳇/达氏鳇）+40~52 金'),
    ('g2',  100000, 2000, 4000, '10 万g~1 吨（剑鱼/波罗的海鲟/六须鲇）+20~40 金'),
    ('g3',       0, 1000, 2000, '<10 万g（各类小鲟）+10~20 金');

CREATE TABLE IF NOT EXISTS cafe_bounty_pool (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    level TEXT NOT NULL,
    slot INTEGER NOT NULL,
    variant INTEGER NOT NULL,
    species TEXT NOT NULL,
    name_zh TEXT NOT NULL,
    weight_g INTEGER NOT NULL,
    grade INTEGER NOT NULL,
    rarity REAL NOT NULL,
    is_rare INTEGER NOT NULL DEFAULT 0,
    is_giant INTEGER NOT NULL DEFAULT 0,
    gold_cents INTEGER NOT NULL,
    subsidy_cents INTEGER NOT NULL DEFAULT 0,
    period_seconds INTEGER NOT NULL,
    phase_seconds INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_cafe_bounty_pool_level_slot
    ON cafe_bounty_pool(level, slot, variant);
