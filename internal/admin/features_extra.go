package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"rf4server/internal/store"
)

// ---- 玩家密码（简化版：只存 SHA1，不建账号体系） ----

// ---- 玩家绑定 IP（与 gateway 共用 gateway_ip_uids.json） ----

// ipBindingsFile 必须与 internal/gateway/login.go 使用的文件名一致。
const ipBindingsFile = "gateway_ip_uids.json"

// ipBindingsMu 仅保护 admin 端对该文件的读写；gateway 端有自己的进程内锁。
var ipBindingsMu sync.Mutex

type ipBinding struct {
	IP  string `json:"ip"`
	UID int64  `json:"uid"`
}

// loadIPBindings 读取 IP→uid 映射；文件不存在视作空映射。
func loadIPBindings() (map[string]int64, error) {
	b, err := os.ReadFile(ipBindingsFile)
	if os.IsNotExist(err) {
		return map[string]int64{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]int64{}
	if strings.TrimSpace(string(b)) == "" {
		return m, nil
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("IP 绑定文件格式错误: %w", err)
	}
	if m == nil {
		m = map[string]int64{}
	}
	return m, nil
}

// saveIPBindings 以 gateway 兼容的 JSON 对象格式写回文件。
func saveIPBindings(m map[string]int64) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ipBindingsFile, b, 0o644)
}

// handleIPBindingsList 返回全部 IP 绑定，按 IP 排序保证结果稳定。
func (s *Server) handleIPBindingsList(w http.ResponseWriter, r *http.Request) {
	ipBindingsMu.Lock()
	defer ipBindingsMu.Unlock()
	m, err := loadIPBindings()
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	bindings := make([]ipBinding, 0, len(m))
	for ip, uid := range m {
		bindings = append(bindings, ipBinding{IP: ip, UID: uid})
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].IP < bindings[j].IP })
	writeJSON(w, map[string]any{"bindings": bindings})
}

// handleIPBindingSet 新增/修改/删除一条 IP 绑定。uid<=0 表示删除该 IP 的映射。
func (s *Server) handleIPBindingSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP  string `json:"ip"`
		UID int64  `json:"uid"`
	}
	if err := readJSON(r, &req); err != nil {
		httpError(w, 400, "参数错误")
		return
	}
	req.IP = strings.TrimSpace(req.IP)
	if req.IP == "" || strings.ContainsAny(req.IP, "/\\") {
		httpError(w, 400, "IP 不能为空且不能包含路径分隔符")
		return
	}
	ipBindingsMu.Lock()
	defer ipBindingsMu.Unlock()
	m, err := loadIPBindings()
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if req.UID <= 0 {
		delete(m, req.IP)
	} else {
		m[req.IP] = req.UID
	}
	if err := saveIPBindings(m); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	_ = s.db.AuditLogWrite(r.Context(), adminOf(r), "ip_binding", req.IP, strconv.FormatInt(req.UID, 10))
	writeJSON(w, map[string]any{"ok": true})
}

// ---- 彻底删除玩家 ----

// handlePlayerDelete 删除玩家在所有表中的记录（必须 confirm===true）。
func (s *Server) handlePlayerDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UID     int64 `json:"uid"`
		Confirm bool  `json:"confirm"`
	}
	if err := readJSON(r, &req); err != nil || req.UID <= 0 {
		httpError(w, 400, "参数错误：需要有效的 uid")
		return
	}
	if !req.Confirm {
		httpError(w, 400, "必须确认后才能删除玩家")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := deletePlayerEverywhere(ctx, s.db.SQL, req.UID); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	_ = s.db.AuditLogWrite(ctx, adminOf(r), "player_delete", strconv.FormatInt(req.UID, 10), "all_tables")
	// 若玩家仍在线，通知主服踢下线，避免内存态残留后回写。
	_ = s.db.QueuePush(ctx, mustJSON(map[string]any{"op": "kick", "uid": req.UID}))
	writeJSON(w, map[string]any{"ok": true})
}

// deletePlayerEverywhere 遍历库中所有含 uid 列的表逐表删除，最后删除 profile 本身。
// 表名来自 sqlite_master（非用户输入）；uid 列类型可能是 INTEGER 或 TEXT，
// 统一用参数绑定比较。整个过程在一个事务内完成。
func deletePlayerEverywhere(ctx context.Context, db *sql.DB, uid int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, table := range tables {
		if table == "profile" {
			continue
		}
		var n int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name='uid'`, table).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %q WHERE uid=?`, table), uid); err != nil {
			return fmt.Errorf("删除 %s 失败: %w", table, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM profile WHERE uid=?`, uid); err != nil {
		return fmt.Errorf("删除 profile 失败: %w", err)
	}
	return tx.Commit()
}

// ---- 邮件模板 ----

type mailTemplate struct {
	ID              int64   `json:"id"`
	Name            string  `json:"name"`
	Subject         string  `json:"subject"`
	Body            string  `json:"body"`
	AttachmentsJSON string  `json:"attachments_json"`
	CreatedTS       float64 `json:"created_ts"`
}

// handleMailTemplateList 列出全部邮件模板（按 id 倒序）。
func (s *Server) handleMailTemplateList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := s.db.SQL.QueryContext(ctx,
		`SELECT id, name, subject, body, attachments_json, created_ts FROM mail_templates ORDER BY id DESC`)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	list := make([]mailTemplate, 0)
	for rows.Next() {
		var t mailTemplate
		if err := rows.Scan(&t.ID, &t.Name, &t.Subject, &t.Body, &t.AttachmentsJSON, &t.CreatedTS); err != nil {
			httpError(w, 500, err.Error())
			return
		}
		list = append(list, t)
	}
	if err := rows.Err(); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"templates": list})
}

// handleMailTemplateSave 新建（id=0）或更新（id>0）邮件模板。
func (s *Server) handleMailTemplateSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID              int64  `json:"id"`
		Name            string `json:"name"`
		Subject         string `json:"subject"`
		Body            string `json:"body"`
		AttachmentsJSON string `json:"attachments_json"`
	}
	if err := readJSON(r, &req); err != nil {
		httpError(w, 400, "参数错误")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httpError(w, 400, "模板名称不能为空")
		return
	}
	if strings.TrimSpace(req.AttachmentsJSON) == "" {
		req.AttachmentsJSON = "[]"
	}
	// 附件必须是 store.MailItem 形状的 JSON 数组，避免保存后群发才发现格式错误。
	var items []store.MailItem
	if err := json.Unmarshal([]byte(req.AttachmentsJSON), &items); err != nil {
		httpError(w, 400, "附件必须是合法的 JSON 数组")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var id int64
	if req.ID > 0 {
		if _, err := s.db.SQL.ExecContext(ctx,
			`UPDATE mail_templates SET name=?, subject=?, body=?, attachments_json=? WHERE id=?`,
			req.Name, req.Subject, req.Body, req.AttachmentsJSON, req.ID); err != nil {
			httpError(w, 500, err.Error())
			return
		}
		id = req.ID
	} else {
		res, err := s.db.SQL.ExecContext(ctx,
			`INSERT INTO mail_templates(name, subject, body, attachments_json, created_ts) VALUES(?,?,?,?,?)`,
			req.Name, req.Subject, req.Body, req.AttachmentsJSON, float64(time.Now().Unix()))
		if err != nil {
			httpError(w, 500, err.Error())
			return
		}
		id, _ = res.LastInsertId()
	}
	_ = s.db.AuditLogWrite(ctx, adminOf(r), "mail_template_save", strconv.FormatInt(id, 10), req.Name)
	writeJSON(w, map[string]any{"ok": true, "id": id})
}

// handleMailTemplateDelete 删除指定邮件模板。
func (s *Server) handleMailTemplateDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := readJSON(r, &req); err != nil || req.ID <= 0 {
		httpError(w, 400, "参数错误：需要有效的 id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if _, err := s.db.SQL.ExecContext(ctx, `DELETE FROM mail_templates WHERE id=?`, req.ID); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	_ = s.db.AuditLogWrite(ctx, adminOf(r), "mail_template_delete", strconv.FormatInt(req.ID, 10), "")
	writeJSON(w, map[string]any{"ok": true})
}
