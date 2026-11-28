-- 改号重放兼容：cafe_bounty_pool.quantity 列若已由旧编号 00075 建过则跳过 ALTER。
ALTER TABLE cafe_bounty_pool ADD COLUMN quantity INTEGER NOT NULL DEFAULT 1;
