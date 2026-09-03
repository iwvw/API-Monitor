package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

// ==================== 存储桶 ====================

func (s *Service) listBuckets(ctx context.Context, c *client, projectID string) ([]normalBucket, error) {
	query := url.Values{}
	query.Set("project", projectID)
	var items []normalBucket
	err := c.listJSON(ctx, http.MethodGet, "storage", "b", query, "items", nil, func(raw json.RawMessage) error {
		items = append(items, bucketFromRaw(raw))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func bucketFromRaw(raw json.RawMessage) normalBucket {
	var bucket struct {
		Name         string                 `json:"name"`
		Location     string                 `json:"location"`
		StorageClass string                 `json:"storageClass"`
		TimeCreated  string                 `json:"timeCreated"`
		Versioning   map[string]interface{} `json:"versioning"`
		Labels       map[string]string      `json:"labels"`
	}
	_ = json.Unmarshal(raw, &bucket)
	return normalBucket{
		Name:         bucket.Name,
		Location:     bucket.Location,
		StorageClass: bucket.StorageClass,
		TimeCreated:  bucket.TimeCreated,
		Versioning:   bucket.Versioning,
		Labels:       bucket.Labels,
	}
}

func (s *Service) createBucketOp(ctx context.Context, c *client, projectID string, r *http.Request) (normalBucket, error) {
	var payload struct {
		Name         string `json:"name"`
		Location     string `json:"location"`
		StorageClass string `json:"storageClass"`
		Versioning   bool   `json:"versioning"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		return normalBucket{}, err
	}
	if payload.Name == "" {
		return normalBucket{}, errFieldRequired
	}
	query := url.Values{}
	if projectID != "" {
		query.Set("project", projectID)
	}
	body := map[string]interface{}{
		"name": payload.Name,
	}
	if payload.Location != "" {
		body["location"] = payload.Location
	}
	if payload.StorageClass != "" {
		body["storageClass"] = payload.StorageClass
	}
	body["versioning"] = map[string]interface{}{"enabled": payload.Versioning}
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodPost, "storage", "b", query, body, &raw); err != nil {
		return normalBucket{}, err
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return normalBucket{}, err
	}
	return bucketFromRaw(encoded), nil
}

func (s *Service) deleteBucketOp(ctx context.Context, c *client, bucket string) error {
	if bucket == "" {
		return errFieldRequired
	}
	return c.do(ctx, http.MethodDelete, "storage", "b/"+bucket, nil, nil, nil)
}

// ==================== 对象 ====================

func (s *Service) listObjects(ctx context.Context, c *client, bucket, prefix string) ([]normalObject, error) {
	query := url.Values{}
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	var items []normalObject
	err := c.listJSON(ctx, http.MethodGet, "storage", "b/"+bucket+"/o", query, "items", nil, func(raw json.RawMessage) error {
		var object struct {
			Name        string `json:"name"`
			Size        string `json:"size"`
			ContentType string `json:"contentType"`
			TimeCreated string `json:"timeCreated"`
			Updated     string `json:"updated"`
		}
		_ = json.Unmarshal(raw, &object)
		items = append(items, normalObject{
			Name:        object.Name,
			Size:        object.Size,
			ContentType: object.ContentType,
			TimeCreated: object.TimeCreated,
			Updated:     object.Updated,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// objectDownloadURLOp 返回 GCS 对象直链（Storage 对象如为公开读可直接访问；
// 私有桶由前端按需走凭证代理，本接口不嵌入 token）。
func (s *Service) objectDownloadURLOp(ctx context.Context, c *client, bucket, object string) (map[string]string, error) {
	if bucket == "" || object == "" {
		return nil, errFieldRequired
	}
	return map[string]string{
		"url":    "https://storage.googleapis.com/" + bucket + "/" + object,
		"bucket": bucket,
		"object": object,
	}, nil
}

// downloadObjectOp 用凭证 token 拉取对象媒体字节（代理流，私有桶可用）。
func (s *Service) downloadObjectOp(ctx context.Context, c *client, bucket, object string) ([]byte, string, error) {
	if bucket == "" || object == "" {
		return nil, "", errFieldRequired
	}
	return c.downloadMedia(ctx, bucket, object)
}

func (s *Service) deleteObjectOp(ctx context.Context, c *client, bucket, object string) error {
	if bucket == "" || object == "" {
		return errFieldRequired
	}
	query := url.Values{}
	return c.do(ctx, http.MethodDelete, "storage", "b/"+bucket+"/o/"+objectWithSlash(object), query, nil, nil)
}

// objectWithSlash 保留对象名中的斜杠（GCS 对象名可为路径）。
func objectWithSlash(object string) string {
	return strings.ReplaceAll(object, "/", "%2F")
}

// ==================== HTTP handler ====================

func (s *Service) buckets(w http.ResponseWriter, r *http.Request, idText, projectID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	items, err := s.listBuckets(r.Context(), client, projectID)
	writeResult(w, map[string]interface{}{"buckets": items}, err)
}

func (s *Service) createBucket(w http.ResponseWriter, r *http.Request, idText string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	projectID := r.URL.Query().Get("projectId")
	item, err := s.createBucketOp(r.Context(), client, projectID, r)
	writeResult(w, map[string]interface{}{"bucket": item}, err)
}

func (s *Service) deleteBucket(w http.ResponseWriter, r *http.Request, idText, bucket string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	writeResult(w, map[string]interface{}{"bucket": bucket}, s.deleteBucketOp(r.Context(), client, bucket))
}

func (s *Service) objects(w http.ResponseWriter, r *http.Request, idText, bucket string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	switch r.Method {
	case http.MethodGet:
		client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
		if !ok {
			return
		}
		items, err := s.listObjects(r.Context(), client, bucket, r.URL.Query().Get("prefix"))
		writeResult(w, map[string]interface{}{"objects": items}, err)
	case http.MethodPost:
		client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
		if !ok {
			return
		}
		item, err := s.uploadObject(r.Context(), client, bucket, r, timeutil.LocationFromSettings(r.Context(), db))
		writeResult(w, map[string]interface{}{"object": item}, err)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// uploadObject 接收 multipart/form-data 的 file 字段并上传到 GCS（media upload）。
func (s *Service) uploadObject(ctx context.Context, c *client, bucket string, r *http.Request, loc *time.Location) (normalObject, error) {
	objectName := r.URL.Query().Get("name")
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 32<<20+1))
	if err != nil {
		return normalObject{}, err
	}
	r.Body.Close()
	if len(data) == 0 {
		return normalObject{}, errors.New("上传内容为空")
	}
	if len(data) > 32<<20 {
		return normalObject{}, errors.New("上传内容超过 32MB 上限")
	}
	if objectName == "" {
		objectName = "uploaded-" + time.Now().In(loc).Format("20060102-150405")
	}
	if err := c.uploadRaw(ctx, bucket, objectName, contentType, data); err != nil {
		return normalObject{}, err
	}
	return normalObject{Name: objectName, Size: fmt.Sprintf("%d", len(data)), ContentType: contentType}, nil
}

func (s *Service) objectDownloadURL(w http.ResponseWriter, r *http.Request, idText, bucket, object string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	item, err := s.objectDownloadURLOp(r.Context(), client, bucket, object)
	writeResult(w, item, err)
}

func (s *Service) deleteObject(w http.ResponseWriter, r *http.Request, idText, bucket, object string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	writeResult(w, map[string]interface{}{"bucket": bucket, "object": object}, s.deleteObjectOp(r.Context(), client, bucket, object))
}

// objectDownload 代理流式下载对象媒体（Bearer token 鉴权，直链对私有桶无效时使用）。
func (s *Service) objectDownload(w http.ResponseWriter, r *http.Request, idText, bucket, object string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	data, contentType, err := s.downloadObjectOp(r.Context(), client, bucket, object)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", contentType)
	if contentType == "" {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	fileName := object
	if index := strings.LastIndex(object, "/"); index >= 0 {
		fileName = object[index+1:]
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+strings.ReplaceAll(fileName, "\"", "")+"\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}