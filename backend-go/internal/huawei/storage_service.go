package huawei

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) buckets(w http.ResponseWriter, r *http.Request, accountID, projectID string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		buckets, err := s.listBucketsForProjects(r.Context(), c, projectID)
		if err != nil {
			response.Error(w, http.StatusBadGateway, "获取 OBS 桶列表失败："+err.Error())
			return
		}
		response.OK(w, buckets)
	case http.MethodPost:
		var payload struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(r, &payload); err != nil {
			response.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if payload.Name == "" {
			response.Error(w, http.StatusBadRequest, "请填写桶名称")
			return
		}
		region, err := s.regionForProject(r.Context(), c, projectID)
		if err != nil {
			response.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		if _, err := c.obsRequest(r.Context(), region, http.MethodPut, payload.Name, "", nil, "", nil); err != nil {
			response.Error(w, http.StatusBadGateway, "创建 OBS 桶失败："+err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"name": payload.Name})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// listBucketsForProjects 列桶：projectID 为 all 时遍历所有项目区域聚合去重。
func (s *Service) listBucketsForProjects(ctx context.Context, c *client, projectID string) ([]normalBucket, error) {
	projects, err := s.fetchProjects(ctx, c)
	if err != nil {
		return nil, err
	}
	if projectID != "all" && projectID != "" {
		for _, project := range projects {
			if project.ProjectID == projectID {
				return listOBSBuckets(ctx, c, project.Name)
			}
		}
	}
	seen := map[string]bool{}
	var all []normalBucket
	for _, project := range projects {
		buckets, err := listOBSBuckets(ctx, c, project.Name)
		if err != nil {
			continue
		}
		for _, bucket := range buckets {
			if bucket.Name != "" && seen[bucket.Name] {
				continue
			}
			if bucket.Name != "" {
				seen[bucket.Name] = true
			}
			all = append(all, bucket)
		}
	}
	return all, nil
}

func listOBSBuckets(ctx context.Context, c *client, region string) ([]normalBucket, error) {
	payload, err := c.obsRequest(ctx, region, http.MethodGet, "", "", nil, "", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Buckets struct {
			Bucket []struct {
				Name         string `xml:"Name"`
				CreationDate string `xml:"CreationDate"`
				Location     string `xml:"Location"`
			} `xml:"Bucket"`
		} `xml:"Buckets"`
	}
	if err := xml.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	buckets := make([]normalBucket, 0, len(result.Buckets.Bucket))
	for _, bucket := range result.Buckets.Bucket {
		buckets = append(buckets, normalBucket{
			Name:      bucket.Name,
			Region:    region,
			CreatedAt: bucket.CreationDate,
		})
	}
	return buckets, nil
}

func (s *Service) deleteBucket(w http.ResponseWriter, r *http.Request, accountID, projectID, bucket string) {
	if !isBucketName(bucket) {
		response.Error(w, http.StatusBadRequest, "非法桶名称")
		return
	}
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
	if !ok {
		return
	}
	region, err := s.obsRegionFor(r.Context(), c, projectID, r.URL.Query().Get("region"))
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	if _, err := c.obsRequest(r.Context(), region, http.MethodDelete, bucket, "", nil, "", nil); err != nil {
		response.Error(w, http.StatusBadGateway, "删除 OBS 桶失败："+err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"bucket": bucket})
}

func (s *Service) objects(w http.ResponseWriter, r *http.Request, accountID, projectID, bucket string) {
	if !isBucketName(bucket) {
		response.Error(w, http.StatusBadRequest, "非法桶名称")
		return
	}
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
	if !ok {
		return
	}
	region, err := s.obsRegionFor(r.Context(), c, projectID, r.URL.Query().Get("region"))
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		query := url.Values{}
		if prefix := r.URL.Query().Get("prefix"); prefix != "" {
			query.Set("prefix", prefix)
		}
		if marker := r.URL.Query().Get("marker"); marker != "" {
			query.Set("marker", marker)
		}
		query.Set("max-keys", "1000")
		payload, err := c.obsRequest(r.Context(), region, http.MethodGet, bucket, "", query, "", nil)
		if err != nil {
			response.Error(w, http.StatusBadGateway, "获取对象列表失败："+err.Error())
			return
		}
		var result struct {
			Contents []struct {
				Key          string `xml:"Key"`
				Size         int64  `xml:"Size"`
				LastModified string `xml:"LastModified"`
			} `xml:"Contents"`
			IsTruncated bool   `xml:"IsTruncated"`
			NextMarker  string `xml:"NextMarker"`
		}
		if err := xml.Unmarshal(payload, &result); err != nil {
			response.Error(w, http.StatusBadGateway, "解析对象列表失败："+err.Error())
			return
		}
		objects := make([]normalObject, 0, len(result.Contents))
		for _, item := range result.Contents {
			objects = append(objects, normalObject{
				Name: item.Key, Size: item.Size, LastModified: item.LastModified,
			})
		}
		response.OK(w, map[string]interface{}{
			"objects": objects, "truncated": result.IsTruncated,
			"nextMarker": result.NextMarker,
		})
	case http.MethodPost:
		objectName := r.URL.Query().Get("name")
		if objectName == "" {
			response.Error(w, http.StatusBadRequest, "缺少对象名称参数 name")
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
		if err != nil {
			response.Error(w, http.StatusBadRequest, "读取上传内容失败："+err.Error())
			return
		}
		contentType := r.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		if _, err := c.obsRequest(r.Context(), region, http.MethodPut, bucket, objectName, nil, contentType, body); err != nil {
			response.Error(w, http.StatusBadGateway, "上传对象失败："+err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"name": objectName})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) deleteObject(w http.ResponseWriter, r *http.Request, accountID, projectID, bucket, object string) {
	if !isBucketName(bucket) || object == "" {
		response.Error(w, http.StatusBadRequest, "非法参数")
		return
	}
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
	if !ok {
		return
	}
	region, err := s.obsRegionFor(r.Context(), c, projectID, r.URL.Query().Get("region"))
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	if _, err := c.obsRequest(r.Context(), region, http.MethodDelete, bucket, object, nil, "", nil); err != nil {
		response.Error(w, http.StatusBadGateway, "删除对象失败："+err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"object": object})
}

// obsRegionFor 确定 OBS 操作区域：优先查询参数 region，否则按项目解析。
func (s *Service) obsRegionFor(ctx context.Context, c *client, projectID, queryRegion string) (string, error) {
	if queryRegion != "" {
		return queryRegion, nil
	}
	return s.regionForProject(ctx, c, projectID)
}

func isBucketName(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '.' {
			continue
		}
		return false
	}
	return true
}