package onepanel

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// 探测主机是否安装了 1Panel。Agent 侧执行固定只读脚本（task_type 61），
// 输出形如 cmd=1 dir=1 proc=1 port=1 的 key=value 行。
const onePanelProbeTimeout = 20 * time.Second

// onePanelProbeConcurrency 限制并发探测数：探测是每条主机各起一个 Agent 任务，
// 主机很多时一次性全发会给 Agent 侧造成压力。
const onePanelProbeConcurrency = 6

type onePanelProbeItem struct {
	ServerID  string `json:"serverId"`
	Installed bool   `json:"installed"`
	Online    bool   `json:"online"`
	Reason    string `json:"reason,omitempty"`
}

// parseOnePanelProbe 解析 Agent 脚本输出。任一项命中即视为已安装：
// 1Panel 有 Docker 与 systemd 两种部署形态，单看某一项会漏判。
func parseOnePanelProbe(output string) (bool, string) {
	hit := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(value) != "1" {
			continue
		}
		switch strings.TrimSpace(key) {
		case "cmd":
			return true, "1pctl"
		case "dir":
			if hit == "" {
				hit = "/opt/1panel"
			}
		case "proc":
			if hit == "" {
				hit = "1panel-core"
			}
		case "port":
			if hit == "" {
				hit = "port 8888"
			}
		}
	}
	return hit != "", hit
}

// onepanelProbe 批量探测：请求体 {"serverIds":[...]}，缺省则探测全部主机。
// 仅返回探测结果，不依赖 1Panel 连接配置（未配置 API Key 也能判断是否装了 1Panel）。
func (s *Service) onepanelProbe(w http.ResponseWriter, r *http.Request) {
	if s.runner == nil {
		response.Error(w, http.StatusServiceUnavailable, "agent runner not available")
		return
	}

	var serverIDs []string
	if r.Method == http.MethodPost {
		var req struct {
			ServerIDs []string `json:"serverIds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			serverIDs = req.ServerIDs
		}
	}
	if len(serverIDs) == 0 {
		ids, err := s.listServerIDs(r)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		serverIDs = ids
	}

	results := make([]onePanelProbeItem, len(serverIDs))
	sem := make(chan struct{}, onePanelProbeConcurrency)
	var wg sync.WaitGroup
	for i, id := range serverIDs {
		i, id := i, id
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out, err := s.runner.RunOnePanelProbe(id, onePanelProbeTimeout)
			if err != nil {
				results[i] = onePanelProbeItem{
					ServerID: id,
					Online:   !strings.Contains(err.Error(), "agent offline"),
					Reason:   err.Error(),
				}
				return
			}
			installed, reason := parseOnePanelProbe(out)
			results[i] = onePanelProbeItem{
				ServerID:  id,
				Installed: installed,
				Online:    true,
				Reason:    reason,
			}
		}()
	}
	wg.Wait()

	response.OK(w, map[string]interface{}{
		"servers": results,
	})
}

func (s *Service) listServerIDs(r *http.Request) ([]string, error) {
	db, err := s.open(r.Context())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(r.Context(), `SELECT id FROM server_accounts ORDER BY order_index ASC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
