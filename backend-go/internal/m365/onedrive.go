package m365

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// oneDriveQuotaConcurrency 限制同时对 Graph 发起的 drive 查询数，避免用户量大时打爆上游。
const oneDriveQuotaConcurrency = 6

// oneDriveQuotaMissingMarkers 是 Graph 表示“该用户尚未开通 OneDrive”的错误特征。
// 这类用户没有 personal site，属于正常状态而非故障，需要与真实错误区分。
// 只匹配明确的 mysite 措辞，不用泛化的 ItemNotFound：后者多为用户 ID 不存在，
// 静默当成“未开通”会掩盖真实的账号/权限问题。
var oneDriveQuotaMissingMarkers = []string{
	"mysite not found",
	"user's mysite",
}

func isOneDriveNotProvisioned(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range oneDriveQuotaMissingMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// driveQuotaPayload 归一化 drive.quota。已用量优先取 Graph 返回的 used 字段；
// 部分租户不返回 used，此时用 total-remaining 兜底。注意 used 可能大于 total
// （配额被调小后用户仍超额），因此不做上界截断，只保证非负。
func driveQuotaPayload(drive map[string]interface{}) map[string]interface{} {
	quota := objectValue(drive["quota"])
	total := numberValue(quota["total"])
	remaining := numberValue(quota["remaining"])
	used := numberValue(quota["used"])
	if used <= 0 && total > 0 {
		used = total - remaining
	}
	if used < 0 {
		used = 0
	}
	usagePct := float64(0)
	if total > 0 {
		usagePct = (float64(used) / float64(total)) * 100
	}
	return map[string]interface{}{
		"driveId":        stringValue(drive["id"], ""),
		"driveType":      stringValue(drive["driveType"], ""),
		"usedBytes":      used,
		"totalBytes":     total,
		"remainingBytes": remaining,
		"deletedBytes":   numberValue(quota["deleted"]),
		"quotaState":     stringValue(quota["state"], ""),
		"usagePercent":   usagePct,
		"overQuota":      total > 0 && used > total,
		"provisioned":    true,
	}
}

// fetchUserDriveQuota 查询单个用户的 OneDrive 容量。
// 返回 (payload, notProvisioned, err)：未开通 OneDrive 的用户不是错误，
// notProvisioned 为 true 且 err 为 nil，调用方据此生成占位条目。
func (s *Service) fetchUserDriveQuota(ctx context.Context, account accountRecord, userID string) (map[string]interface{}, bool, error) {
	result := map[string]interface{}{}
	path := "/users/" + url.PathEscape(userID) + "/drive?$select=id,driveType,quota"
	if err := s.graphJSON(ctx, account, http.MethodGet, path, nil, nil, &result); err != nil {
		if isOneDriveNotProvisioned(err) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return driveQuotaPayload(result), false, nil
}

// oneDriveUsage 批量返回用户 OneDrive 用量。
// 数据源改为逐用户查询 drive quota：用量报表 API 在开启隐私脱敏的租户上会把
// Owner Principal Name 替换为不可逆哈希并清空显示名，无法关联到具体用户，
// 因此不再使用。逐用户查询并发受限，结果按 userId 建索引。
func (s *Service) oneDriveUsage(w http.ResponseWriter, r *http.Request, idText string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}

	userIDs := normalizeUserIDList(r.URL.Query()["userIds"])
	if len(userIDs) == 0 {
		response.Error(w, http.StatusBadRequest, "userIds is required")
		return
	}

	ctx := r.Context()
	type quotaResult struct {
		userID         string
		payload        map[string]interface{}
		notProvisioned bool
		err            error
	}
	results := make([]quotaResult, len(userIDs))
	semaphore := make(chan struct{}, oneDriveQuotaConcurrency)
	var wg sync.WaitGroup
	for index, userID := range userIDs {
		wg.Add(1)
		go func(index int, userID string) {
			defer wg.Done()
			// 等待并发槽位时也要响应请求取消，否则客户端断开后这些 goroutine
			// 会堵在 channel 上直到全部上游调用结束。
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				results[index] = quotaResult{userID: userID, err: ctx.Err()}
				return
			}
			defer func() { <-semaphore }()
			payload, notProvisioned, fetchErr := s.fetchUserDriveQuota(ctx, account, userID)
			results[index] = quotaResult{
				userID:         userID,
				payload:        payload,
				notProvisioned: notProvisioned,
				err:            fetchErr,
			}
		}(index, userID)
	}
	wg.Wait()

	items := make([]map[string]interface{}, 0, len(results))
	failed := 0
	provisioned := 0
	overQuota := 0
	totalUsed := int64(0)
	totalCapacity := int64(0)
	for _, item := range results {
		entry := map[string]interface{}{"userId": item.userID}
		switch {
		case item.err != nil:
			failed++
			entry["error"] = item.err.Error()
		case item.notProvisioned:
			entry["provisioned"] = false
		default:
			provisioned++
			for key, value := range item.payload {
				entry[key] = value
			}
			if flag, ok := item.payload["overQuota"].(bool); ok && flag {
				overQuota++
			}
			totalUsed += numberValue(item.payload["usedBytes"])
			totalCapacity += numberValue(item.payload["totalBytes"])
		}
		items = append(items, entry)
	}

	response.OK(w, map[string]interface{}{
		"items":         items,
		"count":         len(items),
		"provisioned":   provisioned,
		"notProvisioned": len(items) - provisioned - failed,
		"failed":        failed,
		"overQuota":     overQuota,
		"totalUsedBytes": totalUsed,
		"totalCapacity": totalCapacity,
	})
}

// userDriveQuota 读取单个用户的 OneDrive 实时容量详情。
func (s *Service) userDriveQuota(w http.ResponseWriter, r *http.Request, idText, userID string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	payload, notProvisioned, err := s.fetchUserDriveQuota(r.Context(), account, userID)
	if err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	if notProvisioned {
		response.OK(w, map[string]interface{}{
			"userId":      userID,
			"provisioned": false,
			"message":     "该用户尚未开通 OneDrive",
		})
		return
	}
	payload["userId"] = userID
	response.OK(w, payload)
}

// normalizeUserIDList 支持重复参数与逗号分隔两种写法，去重并限制批量规模，
// 避免单次请求扇出过多上游调用。
func normalizeUserIDList(raw []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(raw))
	for _, value := range raw {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" || seen[trimmed] {
				continue
			}
			seen[trimmed] = true
			result = append(result, trimmed)
			if len(result) >= maxOneDriveBatch {
				return result
			}
		}
	}
	return result
}

const maxOneDriveBatch = 500
