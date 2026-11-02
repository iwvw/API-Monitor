package onepanel

import (
	_ "embed"
	"encoding/json"
	"net/http"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

//go:embed catalog.json
var embeddedCatalog []byte

// serveCatalog 输出内置的 1Panel 官方 API 目录（由官方 swagger 生成）。
// 当快捷操作未覆盖目标操作时，AI 可先查本目录，再用 /{serverId}/proxy 手动执行。
func (s *Service) serveCatalog(w http.ResponseWriter, r *http.Request) {
	var entries []map[string]string
	if err := json.Unmarshal(embeddedCatalog, &entries); err != nil {
		response.Error(w, http.StatusInternalServerError, "catalog corrupt")
		return
	}
	response.OK(w, map[string]interface{}{
		"source":         "1Panel official swagger (github.com/1Panel-dev/1Panel)",
		"swaggerVersion": SwaggerVersion,
		"basePath":       "/api/v2",
		"count":          len(entries),
		"endpoints":      entries,
	})
}
