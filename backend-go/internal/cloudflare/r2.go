package cloudflare

import (
	"archive/zip"
	"encoding/json"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) r2Buckets(w http.ResponseWriter, r *http.Request, accountID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if r.Method == http.MethodGet {
		payload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/r2/buckets", auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		resVal := objectValue(payload["result"])
		buckets := arrayValue(resVal["buckets"])
		if buckets == nil {
			buckets = []interface{}{}
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "buckets": buckets})
	} else if r.Method == http.MethodPost {
		var reqBody struct {
			Name     string `json:"name"`
			Location string `json:"location"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if reqBody.Name == "" {
			response.Error(w, http.StatusBadRequest, "桶名称必填")
			return
		}
		body := map[string]interface{}{"name": reqBody.Name}
		if reqBody.Location != "" && reqBody.Location != "auto" {
			body["location"] = reqBody.Location
		}
		payload, err := s.cfRequest(r.Context(), http.MethodPost, "/accounts/"+url.PathEscape(cfAccountID)+"/r2/buckets", auth, body)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "bucket": payload["result"]})
	} else {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deleteR2Bucket(w http.ResponseWriter, r *http.Request, accountID, bucketName string) {
	if r.Method != http.MethodDelete {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	_, err = s.cfRequest(r.Context(), http.MethodDelete, "/accounts/"+url.PathEscape(cfAccountID)+"/r2/buckets/"+url.PathEscape(bucketName), auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) r2Objects(w http.ResponseWriter, r *http.Request, accountID, bucketName string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	query := url.Values{}
	if p := r.URL.Query().Get("prefix"); p != "" {
		query.Set("prefix", p)
	}
	if c := r.URL.Query().Get("cursor"); c != "" {
		query.Set("cursor", c)
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		query.Set("limit", l)
	}
	if d := r.URL.Query().Get("delimiter"); d != "" {
		query.Set("delimiter", d)
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	resInfo := objectValue(payload["result_info"])
	delimited := arrayValue(resInfo["delimited"])
	if delimited == nil {
		delimited = []interface{}{}
	}
	objects := arrayValue(payload["result"])
	if objects == nil {
		objects = []interface{}{}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":            true,
		"objects":            objects,
		"delimited_prefixes": delimited,
		"cursor":             resInfo["cursor"],
	})
}

func (s *Service) r2Metrics(w http.ResponseWriter, r *http.Request, accountID string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/metrics"
	payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	result := objectValue(payload["result"])
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":          true,
		"standard":         result["standard"],
		"infrequentAccess": result["infrequentAccess"],
	})
}

func (s *Service) r2ObjectMutation(w http.ResponseWriter, r *http.Request, accountID, bucketName, objectKey string) {
	switch r.Method {
	case http.MethodDelete:
		s.deleteR2Object(w, r, accountID, bucketName, objectKey)
	case http.MethodPut:
		s.uploadR2Object(w, r, accountID, bucketName, objectKey)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deleteR2Object(w http.ResponseWriter, r *http.Request, accountID, bucketName, objectKey string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects/" + url.PathEscape(objectKey)
	_, err = s.cfRequest(r.Context(), http.MethodDelete, path, auth, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) uploadR2Object(w http.ResponseWriter, r *http.Request, accountID, bucketName, objectKey string) {
	if strings.Trim(objectKey, "/") == "" {
		response.Error(w, http.StatusBadRequest, "对象路径必填")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects/" + url.PathEscape(objectKey)
	_, _, err = s.cfRawRequest(r.Context(), http.MethodPut, path, auth, "application/json", contentType, r.Body)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"objectKey":  objectKey,
		"bucketName": bucketName,
	})
}

func (s *Service) r2ObjectDownloadInfo(w http.ResponseWriter, r *http.Request, accountID, bucketName, objectKey string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	var publicUrl interface{} = nil
	bucketPayload, err := s.cfRequest(r.Context(), http.MethodGet, "/accounts/"+url.PathEscape(cfAccountID)+"/r2/buckets/"+url.PathEscape(bucketName), auth, nil)
	if err == nil {
		bucketInfo := objectValue(bucketPayload["result"])
		if base, ok := bucketInfo["public_url_base"].(string); ok && base != "" {
			publicUrl = base + "/" + objectKey
		}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"publicUrl":  publicUrl,
		"objectKey":  objectKey,
		"bucketName": bucketName,
	})
}

func (s *Service) r2ObjectDownload(w http.ResponseWriter, r *http.Request, accountID, bucketName, objectKey string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects/" + url.PathEscape(objectKey)
	raw, contentType, err := s.cfRawRequest(r.Context(), http.MethodGet, path, auth, "*/*", "", nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": objectFileName(objectKey)}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Service) r2FolderDownload(w http.ResponseWriter, r *http.Request, accountID, bucketName string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	prefix := r.URL.Query().Get("prefix")
	// 打包时每个对象全量读入内存再写入 zip，内存占用与最大单对象成正比；
	// 超过上限的对象跳过并记录，避免超大对象拖垮进程。
	const maxObjectBytes = 512 << 20
	type objectEntry struct {
		key  string
		size int64
	}
	var objects []objectEntry
	cursor := ""
	for {
		query := url.Values{"limit": {"1000"}}
		if prefix != "" {
			query.Set("prefix", prefix)
		}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects"
		if len(query) > 0 {
			path += "?" + query.Encode()
		}
		payload, err := s.cfRequest(r.Context(), http.MethodGet, path, auth, nil)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
			return
		}
		for _, item := range arrayValue(payload["result"]) {
			if obj, ok := item.(map[string]interface{}); ok {
				if key, ok := obj["key"].(string); ok && key != "" {
					objects = append(objects, objectEntry{key: key, size: int64(numberValue(obj["size"]))})
				}
			}
		}
		resInfo := objectValue(payload["result_info"])
		cursor, _ = resInfo["cursor"].(string)
		if cursor == "" {
			break
		}
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].key < objects[j].key })

	folderName := bucketName
	if base := strings.TrimSuffix(prefix, "/"); base != "" {
		folderName = base
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			folderName = base[idx+1:]
		}
	}
	if folderName == "" {
		folderName = bucketName
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": folderName + ".zip"}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	zw := zip.NewWriter(w)
	defer zw.Close()
	ctx := r.Context()
	downloaded, skipped, failed := 0, 0, 0
	for _, object := range objects {
		if object.size > maxObjectBytes {
			skipped++
			applog.Warn(ctx, "cloudflare", "r2 folder download skipped oversized object", "bucket", bucketName, "key", object.key, "size_bytes", object.size, "max_bytes", maxObjectBytes)
			continue
		}
		path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects/" + url.PathEscape(object.key)
		raw, _, err := s.cfRawRequest(ctx, http.MethodGet, path, auth, "*/*", "", nil)
		if err != nil {
			failed++
			applog.Warn(ctx, "cloudflare", "r2 folder download object failed", "bucket", bucketName, "key", object.key, "error", err.Error())
			continue
		}
		rel := object.key
		if prefix != "" {
			rel = strings.TrimPrefix(object.key, prefix)
		}
		// key 恰好等于 prefix 的对象是目录标记（空对象），打包无意义且会产生空名称条目。
		if rel == "" {
			skipped++
			continue
		}
		// R2 对象 key 可含任意字符（含 ../ 与 / 前缀），直接作为 zip 条目名会在
		// 解压时逃逸目标目录（zip-slip）。拒绝此类条目，跳过并记录。
		if !zipEntryNameSafe(rel) {
			skipped++
			applog.Warn(ctx, "cloudflare", "r2 folder download skipped unsafe entry name", "bucket", bucketName, "key", object.key, "entry", rel)
			continue
		}
		header := &zip.FileHeader{Name: rel, Method: zip.Deflate}
		header.SetModTime(time.Now())
		entry, err := zw.CreateHeader(header)
		if err != nil {
			return
		}
		if _, err := entry.Write(raw); err != nil {
			return
		}
		downloaded++
	}
	if skipped > 0 || failed > 0 {
		applog.Warn(ctx, "cloudflare", "r2 folder download completed with skips", "bucket", bucketName, "prefix", prefix, "downloaded", downloaded, "skipped", skipped, "failed", failed)
	}
}

// zipEntryNameSafe 报告相对路径能否安全作为 zip 条目名：
// 拒绝绝对路径（/ 开头）与任何为 ".." 的路径段（/ 与 \ 均视为分隔符），
// 防止解压时条目逃逸目标目录。

func zipEntryNameSafe(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") {
		return false
	}
	for _, segment := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return false
		}
	}
	return true
}

func (s *Service) r2ObjectPreview(w http.ResponseWriter, r *http.Request, accountID, bucketName, objectKey string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	path := "/accounts/" + url.PathEscape(cfAccountID) + "/r2/buckets/" + url.PathEscape(bucketName) + "/objects/" + url.PathEscape(objectKey)
	raw, contentType, err := s.cfRawRequest(r.Context(), http.MethodGet, path, auth, "*/*", "", nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}
	if contentType == "" || strings.HasPrefix(strings.ToLower(contentType), "application/octet-stream") {
		if detected := mime.TypeByExtension(strings.ToLower(pathExt(objectKey))); detected != "" {
			contentType = detected
		} else if len(raw) > 0 {
			contentType = http.DetectContentType(raw)
		} else {
			contentType = "application/octet-stream"
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": objectFileName(objectKey)}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func pathExt(value string) string {
	name := objectFileName(value)
	index := strings.LastIndex(name, ".")
	if index < 0 {
		return ""
	}
	return name[index:]
}

func objectFileName(value string) string {
	trimmed := strings.Trim(value, "/")
	if trimmed == "" {
		return "object"
	}
	parts := strings.Split(trimmed, "/")
	name := parts[len(parts)-1]
	if name == "" {
		return "object"
	}
	return name
}

// Tunnel Management
