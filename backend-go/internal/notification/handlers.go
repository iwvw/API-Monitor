package notification

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) listChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.LoadChannels(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, channels)
}

func (s *Service) getChannel(w http.ResponseWriter, r *http.Request, id string) {
	channel, ok, err := s.LoadChannel(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "channel not found")
		return
	}
	response.OK(w, channel)
}

func (s *Service) createChannel(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	channel, err := s.CreateChannel(r.Context(), payload)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidInput) {
			status = http.StatusBadRequest
		}
		response.Error(w, status, err.Error())
		return
	}
	response.OK(w, channel)
}

func (s *Service) updateChannel(w http.ResponseWriter, r *http.Request, id string) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	if err := s.UpdateChannel(r.Context(), id, payload); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) deleteChannel(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.DeleteChannel(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) testChannel(w http.ResponseWriter, r *http.Request, id string) {
	channel, ok, err := s.loadStoredChannel(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "channel not found")
		return
	}
	config := decryptConfig(channel.ConfigRaw)
	title := "🧪 通知渠道连通性测试"
	loc, _ := s.systemLocation(r.Context())
	message := fmt.Sprintf("状态: 配置有效\n发送时间: %s", time.Now().In(loc).Format(time.RFC3339))
	if _, err := s.sendToChannel(r.Context(), channel, config, title, message); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "test message sent"})
}

func (s *Service) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.LoadRules(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, rules)
}

func (s *Service) getRule(w http.ResponseWriter, r *http.Request, id string) {
	rule, ok, err := s.LoadRule(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "rule not found")
		return
	}
	response.OK(w, rule)
}

func (s *Service) createRule(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	rule, err := s.CreateRule(r.Context(), payload)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidInput) {
			status = http.StatusBadRequest
		}
		response.Error(w, status, err.Error())
		return
	}
	response.OK(w, rule)
}

func (s *Service) updateRule(w http.ResponseWriter, r *http.Request, id string) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	if err := s.UpdateRule(r.Context(), id, payload); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidInput) {
			status = http.StatusBadRequest
		}
		response.Error(w, status, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) deleteRule(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.DeleteRule(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) setRuleEnabled(w http.ResponseWriter, r *http.Request, id string, enabled bool) {
	if err := s.SetRuleEnabled(r.Context(), id, enabled); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) previewTemplate(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	data := objectValue(payload["data"])
	rule := Rule{
		Name:            "Template Preview",
		Severity:        stringDefault(data["severity"], "info"),
		EventType:       stringDefault(data["eventType"], "preview"),
		TitleTemplate:   stringValue(payload["title_template"]),
		MessageTemplate: stringValue(payload["message_template"]),
	}
	loc, _ := s.systemLocation(r.Context())
	templateData := notificationTemplateData(data, loc)
	response.OK(w, map[string]interface{}{
		"title":     formatTitle(rule, data),
		"message":   formatMessage(rule, data, loc),
		"variables": sortedKeys(templateData),
	})
}

func (s *Service) dryRunRule(w http.ResponseWriter, r *http.Request, id string) {
	rule, ok, err := s.LoadRule(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "rule not found")
		return
	}
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	data := objectValue(payload["data"])
	result, err := s.DryRun(r.Context(), rule, data)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, result)
}

func (s *Service) listHistory(w http.ResponseWriter, r *http.Request) {
	limit := boundedLimit(r.URL.Query().Get("limit"))
	history, err := s.LoadHistory(r.Context(), r.URL.Query().Get("status"), limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, history)
}

func (s *Service) clearHistory(w http.ResponseWriter, r *http.Request) {
	if err := s.ClearHistory(r.Context()); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.LoadConfig(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, cfg)
}

func (s *Service) updateConfig(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	if err := s.UpdateConfig(r.Context(), payload); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) trigger(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	sourceModule := stringValue(payload["source_module"])
	eventType := stringValue(payload["event_type"])
	if sourceModule == "" || eventType == "" {
		response.Error(w, http.StatusBadRequest, "missing required parameters")
		return
	}
	if err := s.Trigger(r.Context(), sourceModule, eventType, objectValue(payload["data"])); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "alert triggered"})
}