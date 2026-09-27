package bookmarks

import (
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// 网址导航公开页的自定义背景图存储。
//
// 与 settings 的 site-brand 图标同构（元数据 JSON + 资源文件），但：
//   - 体积上限更大（背景图通常比图标大得多）
//   - 只接受位图，不接受 SVG（背景不需要矢量，且 SVG 可携带脚本）
//   - 通过 /site-brand-backgrounds/<id> 公开访问（免登录，公开页要用）

const (
	maxBackgroundImageBytes = 8 * 1024 * 1024 // 8MB
	backgroundUploadFormMax = 9 * 1024 * 1024
)

// backgroundMetaMu 串行化「读元数据 → 改 → 写回」。
//
// backgrounds.json 是整份读出来再整体覆盖写的，多个上传/删除并发时
// 后写的那份会基于旧快照落盘，把先写的那条记录整条抹掉。
var backgroundMetaMu sync.Mutex

type backgroundImageRecord struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Ext         string `json:"ext"`
	ContentType string `json:"content_type"`
	CreatedAt   string `json:"created_at"`
	Size        int64  `json:"size"`
}

func (s *Service) backgroundDir() string {
	return filepath.Join(s.cfg.DataDir, "site-brand", "backgrounds")
}

func (s *Service) backgroundMetaPath() string {
	return filepath.Join(s.cfg.DataDir, "site-brand", "backgrounds.json")
}

// --- HTTP ---

func (s *Service) listBackgroundImages(w http.ResponseWriter, r *http.Request) {
	records, err := s.loadBackgroundRecords()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	sort.Slice(records, func(i, j int) bool { return records[i].CreatedAt > records[j].CreatedAt })
	items := make([]map[string]interface{}, 0, len(records))
	for _, item := range records {
		items = append(items, backgroundResponse(item))
	}
	response.OK(w, items)
}

func (s *Service) uploadBackgroundImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, backgroundUploadFormMax)
	if err := r.ParseMultipartForm(backgroundUploadFormMax); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid upload payload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "missing upload file")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxBackgroundImageBytes+1))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "read upload failed")
		return
	}
	if len(data) == 0 {
		response.Error(w, http.StatusBadRequest, "empty upload file")
		return
	}
	if len(data) > maxBackgroundImageBytes {
		response.Error(w, http.StatusBadRequest, fmt.Sprintf("image exceeds %d MB", maxBackgroundImageBytes/(1024*1024)))
		return
	}

	ext, contentType, err := detectBackgroundFormat(data)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = strings.TrimSpace(header.Filename)
	}
	if name == "" {
		name = "背景图"
	}

	record, err := s.saveBackgroundImage(data, name, ext, contentType)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, backgroundResponse(record))
}

func (s *Service) deleteBackgroundImage(w http.ResponseWriter, r *http.Request, id string) {
	recordID := strings.TrimSpace(id)
	if recordID == "" {
		response.Error(w, http.StatusNotFound, "background not found")
		return
	}
	backgroundMetaMu.Lock()
	defer backgroundMetaMu.Unlock()

	records, err := s.loadBackgroundRecords()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	index := -1
	var record backgroundImageRecord
	for i, item := range records {
		if item.ID == recordID {
			index, record = i, item
			break
		}
	}
	if index < 0 {
		response.Error(w, http.StatusNotFound, "background not found")
		return
	}
	if err := os.Remove(filepath.Join(s.backgroundDir(), record.ID+record.Ext)); err != nil && !os.IsNotExist(err) {
		response.Error(w, http.StatusInternalServerError, "delete background asset failed")
		return
	}
	records = append(records[:index], records[index+1:]...)
	if err := s.saveBackgroundRecords(records); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]string{"id": record.ID})
}

// ServePublicBackgroundAsset 服务 /site-brand-backgrounds/<id>，公开可读。
func (s *Service) ServePublicBackgroundAsset(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if !strings.HasPrefix(r.URL.Path, "/site-brand-backgrounds/") {
		return false
	}
	// 允许带扩展名（前端生成的地址可能带后缀），取第一段作为 id
	raw := strings.TrimPrefix(r.URL.Path, "/site-brand-backgrounds/")
	id := strings.SplitN(strings.Trim(raw, "/"), ".", 2)[0]
	record, path, ok, err := s.findBackgroundImage(id)
	if err != nil || !ok {
		return false
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if record.ContentType != "" {
		w.Header().Set("Content-Type", record.ContentType)
	}
	http.ServeFile(w, r, path)
	return true
}

// --- storage ---

func (s *Service) saveBackgroundImage(data []byte, name, ext, contentType string) (backgroundImageRecord, error) {
	backgroundMetaMu.Lock()
	defer backgroundMetaMu.Unlock()

	records, err := s.loadBackgroundRecords()
	if err != nil {
		return backgroundImageRecord{}, err
	}
	token, err := randomBackgroundToken()
	if err != nil {
		return backgroundImageRecord{}, err
	}
	record := backgroundImageRecord{
		ID:          "bg-" + strings.ToLower(token[:12]),
		Name:        strings.TrimSpace(name),
		Ext:         ext,
		ContentType: contentType,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Size:        int64(len(data)),
	}
	if err := os.MkdirAll(s.backgroundDir(), 0o755); err != nil {
		return backgroundImageRecord{}, fmt.Errorf("prepare background storage: %w", err)
	}
	path := filepath.Join(s.backgroundDir(), record.ID+record.Ext)
	// 原子写：先写临时文件再改名，避免中断留下半张图
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return backgroundImageRecord{}, fmt.Errorf("write background asset: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return backgroundImageRecord{}, fmt.Errorf("finalize background asset: %w", err)
	}
	records = append(records, record)
	if err := s.saveBackgroundRecords(records); err != nil {
		_ = os.Remove(path)
		return backgroundImageRecord{}, err
	}
	return record, nil
}

func (s *Service) findBackgroundImage(id string) (backgroundImageRecord, string, bool, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return backgroundImageRecord{}, "", false, nil
	}
	records, err := s.loadBackgroundRecords()
	if err != nil {
		return backgroundImageRecord{}, "", false, err
	}
	for _, item := range records {
		if item.ID != id {
			continue
		}
		path := filepath.Join(s.backgroundDir(), item.ID+item.Ext)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return backgroundImageRecord{}, "", false, nil
		}
		return item, path, true, nil
	}
	return backgroundImageRecord{}, "", false, nil
}

func (s *Service) loadBackgroundRecords() ([]backgroundImageRecord, error) {
	data, err := os.ReadFile(s.backgroundMetaPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []backgroundImageRecord{}, nil
		}
		return nil, fmt.Errorf("read backgrounds: %w", err)
	}
	var records []backgroundImageRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("decode backgrounds: %w", err)
	}
	filtered := make([]backgroundImageRecord, 0, len(records))
	for _, item := range records {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Ext) == "" {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func (s *Service) saveBackgroundRecords(records []backgroundImageRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.backgroundMetaPath()), 0o755); err != nil {
		return fmt.Errorf("prepare background metadata: %w", err)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode backgrounds: %w", err)
	}
	tmp := s.backgroundMetaPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write background metadata: %w", err)
	}
	if err := os.Rename(tmp, s.backgroundMetaPath()); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace background metadata: %w", err)
	}
	return nil
}

func backgroundResponse(record backgroundImageRecord) map[string]interface{} {
	return map[string]interface{}{
		"id":          record.ID,
		"name":        record.Name,
		"ext":         record.Ext,
		"url":         fmt.Sprintf("/site-brand-backgrounds/%s%s", record.ID, record.Ext),
		"createdAt":   record.CreatedAt,
		"contentType": record.ContentType,
		"size":        record.Size,
	}
}

// detectBackgroundFormat 用魔数判断真实格式；背景只接受位图。
func detectBackgroundFormat(data []byte) (string, string, error) {
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/png":
		return ".png", contentType, nil
	case "image/jpeg":
		return ".jpg", contentType, nil
	case "image/webp":
		return ".webp", contentType, nil
	case "image/gif":
		return ".gif", contentType, nil
	case "image/bmp":
		return ".bmp", contentType, nil
	}
	// DetectContentType 覆盖不到 avif 等，做一次尾部兜底
	if strings.Contains(strings.ToLower(contentType), "image/") {
		return "", "", fmt.Errorf("unsupported image type: %s", contentType)
	}
	return "", "", fmt.Errorf("unsupported file type, only png/jpg/webp/gif/bmp are allowed")
}

func randomBackgroundToken() (string, error) {
	var buf [16]byte
	if _, err := crand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
