-- 邮件模板：管理后台保存后可群发，减少重复填写正文与附件。
CREATE TABLE IF NOT EXISTS mail_templates (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT NOT NULL,
    subject          TEXT NOT NULL DEFAULT '',
    body             TEXT NOT NULL DEFAULT '',
    attachments_json TEXT NOT NULL DEFAULT '[]',
    created_ts       REAL NOT NULL DEFAULT 0
);
