-- 玩家登录密码（简化版，不引入账号注册体系）。
-- 存 SHA1 小写 hex；空串表示免密，登录时跳过校验。
ALTER TABLE profile ADD COLUMN password_sha1 TEXT NOT NULL DEFAULT '';
