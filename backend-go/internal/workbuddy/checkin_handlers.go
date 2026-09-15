package workbuddy

// 本文件是签到 / 活跃上报 / 连登状态的管理面 HTTP 接口。
// 协议调用在 checkin.go，调度在 scheduler.go，这里只做参数校验与结果封装。

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// handleCheckinAll 立即对全部账号执行一次签到（含连登管家）。
func (s *Service) handleCheckinAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	results := s.CheckinAll(ctx)
	responseJSON(w, map[string]interface{}{"success": true, "results": results})
}

// handleActivityAll 立即对全部国内版账号执行一次活跃上报。
func (s *Service) handleActivityAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	results := s.ReportActivityAll(ctx)
	responseJSON(w, map[string]interface{}{"success": true, "results": results})
}

// handleCheckinAccount 对单账号立即执行一次签到（含连登管家）。
func (s *Service) handleCheckinAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.findAccount(strings.TrimSpace(id))
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	result, err := s.runCheckin(ctx, acc)
	if err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "result": result})
}

// handleActivityAccount 对单账号立即执行一次活跃上报。
func (s *Service) handleActivityAccount(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.findAccount(strings.TrimSpace(id))
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := s.reportChatActivity(ctx, acc); err != nil {
		responseJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true})
}

// handleAccountStreak 查询单账号的连登状态（只读，供前端悬浮明细）。
// 国际版账号无连登体系，返回 success=false 并说明原因。
func (s *Service) handleAccountStreak(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.findAccount(strings.TrimSpace(id))
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	if !checkinSupported(acc) {
		responseJSON(w, map[string]interface{}{"success": false, "error": "国际版无签到/连登体系"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	full, err := s.growthStreakFull(ctx, acc)
	if err != nil {
		responseJSON(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "streak": full})
}
