package onepanel

import (
	_ "embed"
	"encoding/json"
	"net/http"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

//go:embed catalog.json
var embeddedCatalog []byte

// 内置 1Panel 官方 API 目录（method / path / summary）。
// 当快捷操作未覆盖目标操作时，AI 可先查询本目录，再用 /{serverId}/proxy 手动执行。
func (s *Service) serveCatalog(w http.ResponseWriter, r *http.Request) {
	var entries []map[string]string
	if err := json.Unmarshal(embeddedCatalog, &entries); err != nil {
		response.Error(w, http.StatusInternalServerError, "catalog corrupt")
		return
	}
	response.OK(w, map[string]interface{}{
		"source":   "1Panel v2 OpenAPI (bundled)",
		"basePath": "/api/v2",
		"count":    len(entries),
		"endpoints": entries,
	})
}