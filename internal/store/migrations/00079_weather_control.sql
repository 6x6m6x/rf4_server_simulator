-- 天气控制：按地图覆盖温度随机区间、风速档位、六种天气抽取权重。
-- 后台「天气调节」页读写本表；服务端判鱼与客户端地图下发共同消费。
-- 未配置的地图走代码默认值（温度基准/浮动见 weather_schedule，风速见 normal 档，
-- 天气权重 3:2:1:1:1:1），保证升级后既有地图行为不变。
CREATE TABLE IF NOT EXISTS weather_control(
    map_key        TEXT PRIMARY KEY,
    temp_base      REAL NOT NULL DEFAULT 0,   -- 温度基准线（°C，整数）；0=沿用模板区间中值
    temp_swing     REAL NOT NULL DEFAULT 0,   -- 上下浮动（°C，整数）；0=沿用模板区间
    wind_mode      TEXT NOT NULL DEFAULT 'normal', -- normal=<5m/s, windy=>=5m/s, calm=0 无风
    kind_weights   TEXT NOT NULL DEFAULT '',  -- 六种天气权重 JSON 数组 [晴,少云,多云,雨,暴雨,雷暴]
    enabled        INTEGER NOT NULL DEFAULT 1,
    updated_ts     REAL NOT NULL DEFAULT 0,
    updated_by     TEXT NOT NULL DEFAULT ''
);
