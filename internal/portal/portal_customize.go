package portal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 服务中心自定义：改名/头像，单价 20 金币（见 store.PortalCustomizePriceGold）。
// 头像文件保存在主库同级的 avatars/ 目录，与后台头像共用同一份文件，
// 通过 /portal/avatars/<uid><ext> 对外提供（客户端资料卡读 /avatars/ 亦可命中后台路由）。

func (s *Server) avatarDir() string {
	return filepath.Join(filepath.Dir(s.db.Path), "avatars")
}

// GET /portal/api/customize → {goldMinor, priceGold}
func (s *Server) customizeStatus(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.identity(w, r)
	if !ok {
		return
	}
	gold, price, err := s.db.PortalCustomizeStatus(r.Context(), uid)
	if err != nil {
		fail(w, 500, "余额读取失败")
		return
	}
	write(w, map[string]any{"goldMinor": gold, "priceGold": price})
}

// POST /portal/api/customize/name {name} → 扣 20 金币改名
func (s *Server) customizeName(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.identity(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		fail(w, 400, "参数无效")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 24 {
		fail(w, 400, "昵称需为 1~24 个字符")
		return
	}
	if err := s.db.PortalRename(r.Context(), uid, req.Name, time.Now().Unix()); err != nil {
		fail(w, 400, err.Error())
		return
	}
	gold, price, _ := s.db.PortalCustomizeStatus(r.Context(), uid)
	write(w, map[string]any{"ok": true, "name": req.Name, "goldMinor": gold, "priceGold": price})
}

// POST /portal/api/customize/avatar （multipart: file）→ 扣 20 金币换头像
func (s *Server) customizeAvatar(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.identity(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		fail(w, 400, "请选择图片（最大 8MB）")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, "请选择图片文件")
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
	default:
		switch strings.ToLower(hdr.Header.Get("Content-Type")) {
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpg"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		default:
			fail(w, 400, "仅支持 png/jpg/gif/webp")
			return
		}
	}
	dir := s.avatarDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(w, 500, "头像目录创建失败")
		return
	}
	name := fmt.Sprintf("%d%s", uid, ext)
	dst, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		fail(w, 500, "头像写入失败")
		return
	}
	if _, err := io.Copy(dst, io.LimitReader(file, 8<<20)); err != nil {
		dst.Close()
		fail(w, 500, "头像写入失败")
		return
	}
	dst.Close()
	// 扣金币并落库；若余额不足，文件已写但数据库不引用，不影响玩家。
	avatar := "/portal/avatars/" + name
	if err := s.db.PortalSetAvatar(r.Context(), uid, avatar, time.Now().Unix()); err != nil {
		fail(w, 400, err.Error())
		return
	}
	gold, price, _ := s.db.PortalCustomizeStatus(r.Context(), uid)
	write(w, map[string]any{"ok": true, "avatar": avatar, "goldMinor": gold, "priceGold": price})
}

// /portal/avatars/<name> 静态托管头像（仅文件名，防目录穿越）。
func (s *Server) serveAvatar(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/portal/avatars/")
	if name == "" || strings.ContainsAny(name, `/\\`) || strings.Contains(name, "..") {
		fail(w, 400, "无效头像路径")
		return
	}
	http.ServeFile(w, r, filepath.Join(s.avatarDir(), name))
}
