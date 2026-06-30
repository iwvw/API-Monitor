package systemlogs

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

type Service struct{ cfg config.Config }

type LogLine struct {
	Raw     string `json:"raw"`
	Time    string `json:"time,omitempty"`
	Level   string `json:"level,omitempty"`
	Message string `json:"message,omitempty"`
	Module  string `json:"module,omitempty"`
	Matched bool   `json:"matched"`
}

func New(cfg config.Config) *Service { return &Service{cfg: cfg} }

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/system/logs"), "/")
	switch {
	case path == "stream" && r.Method == http.MethodGet:
		s.stream(w, r)
	case path == "download" && r.Method == http.MethodGet:
		s.download(w, r)
	default:
		response.Error(w, http.StatusNotFound, "system logs route not implemented")
	}
}

func (s *Service) stream(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 2000 {
		limit = 300
	}
	level := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("level")))
	if level == "ALL" {
		level = ""
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("q"))
	lines, err := readTail(s.logPath(), limit*4)
	if err != nil {
		if os.IsNotExist(err) {
			response.OK(w, map[string]any{"lines": []LogLine{}, "path": s.logPath()})
			return
		}
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	var matcher *regexp.Regexp
	if keyword != "" {
		matcher, _ = regexp.Compile(keyword)
	}
	result := []LogLine{}
	for _, raw := range lines {
		item := parseLine(raw)
		if level != "" && strings.ToUpper(item.Level) != level {
			continue
		}
		if matcher != nil {
			item.Matched = matcher.MatchString(raw)
			if !item.Matched {
				continue
			}
		} else if keyword != "" && !strings.Contains(strings.ToLower(raw), strings.ToLower(keyword)) {
			continue
		}
		result = append(result, item)
	}
	if len(result) > limit {
		result = result[len(result)-limit:]
	}
	response.OK(w, map[string]any{"lines": result, "path": s.logPath()})
}

func (s *Service) download(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, s.logPath())
}

func (s *Service) logPath() string {
	if path := applog.LogPath(); path != "" {
		return path
	}
	return s.cfg.DataDir + string(os.PathSeparator) + "logs" + string(os.PathSeparator) + "app.log"
}

func readTail(path string, limit int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	lines := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > limit {
			copy(lines, lines[len(lines)-limit:])
			lines = lines[:limit]
		}
	}
	return lines, scanner.Err()
}

func parseLine(raw string) LogLine {
	item := LogLine{Raw: raw}
	var obj map[string]any
	if json.Unmarshal([]byte(raw), &obj) == nil {
		item.Time, _ = obj["time"].(string)
		if item.Time == "" {
			item.Time, _ = obj["timestamp"].(string)
		}
		item.Level, _ = obj["level"].(string)
		item.Message, _ = obj["msg"].(string)
		if item.Message == "" {
			item.Message, _ = obj["message"].(string)
		}
		item.Module, _ = obj["module"].(string)
		if item.Message == "http request" {
			parts := []string{}
			if method, _ := obj["method"].(string); method != "" {
				parts = append(parts, method)
			}
			if path, _ := obj["path"].(string); path != "" {
				parts = append(parts, path)
			}
			if status, ok := obj["status"].(float64); ok {
				parts = append(parts, "status="+strconv.Itoa(int(status)))
			}
			if duration, ok := obj["duration_ms"].(float64); ok {
				parts = append(parts, "duration="+strconv.Itoa(int(duration))+"ms")
			}
			if len(parts) > 0 {
				item.Message = strings.Join(parts, " ")
			}
		}
	}
	return item
}
