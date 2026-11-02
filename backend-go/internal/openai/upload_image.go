package openai

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// safeUploadPathJoin 将 /uploads/ 开头的图片 URL 解析为 DataDir/uploads 内的绝对路径。
// 返回 false 表示路径穿越或越界（拒绝内联），防止读取 DataDir 之外的任意文件。
func safeUploadPathJoin(dataDir, imgURL string) (string, bool) {
	if dataDir == "" || !strings.HasPrefix(imgURL, "/uploads/") {
		return "", false
	}
	uploadsRoot := filepath.Clean(filepath.Join(filepath.Clean(dataDir), "uploads"))
	joined := filepath.Clean(filepath.Join(uploadsRoot, strings.TrimPrefix(imgURL, "/uploads/")))
	expected := uploadsRoot
	if !strings.HasSuffix(expected, string(os.PathSeparator)) {
		expected += string(os.PathSeparator)
	}
	if joined != uploadsRoot && !strings.HasPrefix(joined, expected) {
		return "", false
	}
	return joined, true
}

func (s *Service) inlineLocalUploadImage(imgURLMap map[string]interface{}, dataDir string) {
	imgURL, ok := imgURLMap["url"].(string)
	if !ok {
		return
	}
	filePath, ok := safeUploadPathJoin(dataDir, imgURL)
	if !ok {
		return
	}
	if fileBytes, err := os.ReadFile(filePath); err == nil {
		ext := strings.ToLower(filepath.Ext(filePath))
		mimeType := "image/jpeg"
		switch ext {
		case ".png":
			mimeType = "image/png"
		case ".webp":
			mimeType = "image/webp"
		case ".gif":
			mimeType = "image/gif"
		}
		b64 := base64.StdEncoding.EncodeToString(fileBytes)
		imgURLMap["url"] = fmt.Sprintf("data:%s;base64,%s", mimeType, b64)
		imgURLMap["_original_url"] = imgURL
	}
}
