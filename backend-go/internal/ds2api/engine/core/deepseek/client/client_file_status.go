package client

import (
	"context"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

const (
	fileReadyPollAttempts = 60
	fileReadyPollInterval = time.Second
	fileReadyPollTimeout  = 65 * time.Second
)

var fileReadySleep = time.Sleep

// ErrUploadFileNotFound indicates that DeepSeek returned no matching uploaded file.
var ErrUploadFileNotFound = errors.New("uploaded file not found")

func (c *Client) waitForUploadedFile(ctx context.Context, a *auth.RequestAuth, result *UploadFileResult) error {
	if result == nil || strings.TrimSpace(result.ID) == "" {
		return nil
	}
	if isReadyUploadFileStatus(result.Status) {
		return nil
	}

	pollCtx, cancel := context.WithTimeout(ctx, fileReadyPollTimeout)
	defer cancel()

	var lastErr error
	for attempt := 0; attempt < fileReadyPollAttempts; attempt++ {
		if err := pollCtx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("waiting for file %s to become ready: %w", result.ID, lastErr)
			}
			return fmt.Errorf("waiting for file %s to become ready: %w", result.ID, err)
		}

		fetched, err := c.FetchUploadedFile(pollCtx, a, result.ID)
		if err == nil && fetched != nil {
			mergeUploadFileResults(result, fetched)
			if isReadyUploadFileStatus(result.Status) {
				return nil
			}
			lastErr = fmt.Errorf("status=%s", strings.TrimSpace(result.Status))
		} else if err != nil {
			lastErr = err
			config.Logger.Debug("[upload_file] waiting for file readiness", "file_id", result.ID, "attempt", attempt+1, "error", err)
		}

		if attempt < fileReadyPollAttempts-1 {
			fileReadySleep(fileReadyPollInterval)
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("status=%s", strings.TrimSpace(result.Status))
	}
	return fmt.Errorf("file %s did not become ready: %w", result.ID, lastErr)
}

// FetchUploadedFile returns metadata for an uploaded DeepSeek file by ID.
func (c *Client) FetchUploadedFile(ctx context.Context, a *auth.RequestAuth, fileID string) (*UploadFileResult, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return nil, errors.New("file id is required")
	}
	clients := c.requestClientsForAuth(ctx, a)
	reqURL := dsprotocol.DeepSeekFetchFilesURL + "?file_ids=" + url.QueryEscape(fileID)
	headers := c.authHeaders(a.DeepSeekToken, a.Account.Locale)

	resp, status, err := c.getJSONWithStatus(ctx, clients.regular, reqURL, headers)
	if err != nil {
		return nil, err
	}

	code, bizCode, msg, bizMsg := extractResponseStatus(resp)
	if status != http.StatusOK || code != 0 || bizCode != 0 {
		if strings.TrimSpace(bizMsg) != "" {
			msg = bizMsg
		}
		if msg == "" {
			msg = http.StatusText(status)
		}
		return nil, fmt.Errorf("request failed: status=%d, code=%d, msg=%s", status, code, msg)
	}

	result := extractFetchedUploadFileResult(resp, fileID)
	if result == nil || strings.TrimSpace(result.ID) == "" {
		return nil, ErrUploadFileNotFound
	}
	result.Raw = resp
	return result, nil
}

func extractFetchedUploadFileResult(resp map[string]any, targetID string) *UploadFileResult {
	targetID = strings.TrimSpace(targetID)
	if resp == nil || targetID == "" {
		return nil
	}

	var walk func(any) *UploadFileResult
	walk = func(v any) *UploadFileResult {
		switch x := v.(type) {
		case map[string]any:
			if result := buildUploadFileResultFromMap(x, targetID); result != nil {
				return result
			}
			for _, nested := range x {
				if result := walk(nested); result != nil {
					return result
				}
			}
		case []any:
			for _, item := range x {
				if result := walk(item); result != nil {
					return result
				}
			}
		}
		return nil
	}

	if result := walk(resp); result != nil {
		return result
	}
	return nil
}

func buildUploadFileResultFromMap(m map[string]any, targetID string) *UploadFileResult {
	fileID := strings.TrimSpace(firstNonEmptyString(m, "id", "file_id"))
	if fileID == "" || !strings.EqualFold(fileID, targetID) {
		return nil
	}
	result := &UploadFileResult{
		ID:       fileID,
		Filename: firstNonEmptyString(m, "name", "filename", "file_name"),
		Status:   firstNonEmptyString(m, "status", "file_status"),
		Purpose:  firstNonEmptyString(m, "purpose"),
		IsImage:  firstBool(m, "is_image", "isImage"),
		Bytes:    firstPositiveInt64(m, "bytes", "size", "file_size"),
	}
	if result.Status == "" {
		result.Status = "uploaded"
	}
	return result
}

func mergeUploadFileResults(dst, src *UploadFileResult) {
	if dst == nil || src == nil {
		return
	}
	if strings.TrimSpace(src.ID) != "" {
		dst.ID = strings.TrimSpace(src.ID)
	}
	if strings.TrimSpace(src.Filename) != "" {
		dst.Filename = strings.TrimSpace(src.Filename)
	}
	if src.Bytes > 0 {
		dst.Bytes = src.Bytes
	}
	if strings.TrimSpace(src.Status) != "" {
		dst.Status = strings.TrimSpace(src.Status)
	}
	if strings.TrimSpace(src.Purpose) != "" {
		dst.Purpose = strings.TrimSpace(src.Purpose)
	}
	dst.IsImage = src.IsImage
	if len(src.Raw) > 0 {
		dst.Raw = src.Raw
	}
	if src.RawHeaders != nil {
		dst.RawHeaders = src.RawHeaders.Clone()
	}
}

func isReadyUploadFileStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "processed", "ready", "done", "available", "success", "completed", "finished":
		return true
	default:
		return false
	}
}
