package emailcode

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// ServeHTTP 处理 /api/emailcode/* 的全部请求。
// 管理面（收件箱查询/取用）由 manifest 的会话鉴权把关；
// 邮件投递入口（ingest）为公开路由，用共享密钥校验 Worker 身份。
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/emailcode")
	path = "/" + strings.Trim(path, "/")
	switch {
	case path == "/ingest":
		s.handleIngest(w, r)
	case path == "/domains":
		s.handleDomains(w, r)
	case path == "/messages":
		s.handleMessages(w, r)
	case path == "/stats":
		s.handleStats(w, r)
	case strings.HasPrefix(path, "/messages/"):
		s.handleMessageItem(w, r, strings.TrimPrefix(path, "/messages/"))
	case path == "/wait":
		s.handleWait(w, r)
	case path == "/clear":
		s.handleClear(w, r)
	default:
		response.Error(w, http.StatusNotFound, "emailcode route not found")
	}
}

// ingestPayload 是邮件 Worker 投递的报文。
// 新版 Worker 只需回传原件与元数据，提取在面板完成；为兼容旧版仍接受已提取的 code/link。
type ingestPayload struct {
	Mailbox   string `json:"mailbox"`
	To        string `json:"to"`
	Domain    string `json:"domain"`
	Sender    string `json:"sender"`
	From      string `json:"from"`
	Subject   string `json:"subject"`
	MessageID string `json:"messageId"`
	Raw       string `json:"raw"`
	Code      string `json:"code"`
	Link      string `json:"link"`
	Snippet   string `json:"snippet"`
	Received  string `json:"receivedAt"`
}

// handleIngest 接收邮件 Worker 投递的邮件。公开接口，用 X-Worker-Secret 校验。
func (s *Service) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !verifyWorkerSecret(r) {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body ingestPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&body); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	mailbox := strings.TrimSpace(body.Mailbox)
	if mailbox == "" {
		mailbox = strings.TrimSpace(body.To)
	}
	if mailbox == "" {
		response.Error(w, http.StatusBadRequest, "收件人不能为空")
		return
	}
	if !strings.Contains(mailbox, "@") || strings.HasPrefix(mailbox, "@") || strings.HasSuffix(mailbox, "@") {
		response.Error(w, http.StatusBadRequest, "收件人格式无效")
		return
	}
	sender := strings.TrimSpace(body.Sender)
	if sender == "" {
		sender = strings.TrimSpace(body.From)
	}

	msg := Message{
		Mailbox:    mailbox,
		Domain:     strings.TrimSpace(body.Domain),
		Sender:     sender,
		Subject:    strings.TrimSpace(body.Subject),
		MessageID:  strings.TrimSpace(body.MessageID),
		Code:       strings.TrimSpace(body.Code),
		Link:       strings.TrimSpace(body.Link),
		Snippet:    strings.TrimSpace(body.Snippet),
		ReceivedAt: strings.TrimSpace(body.Received),
	}

	// 面板侧解析：用标准库 MIME + charset 解码 + 分层提取，比 Worker 内启发式更准。
	if raw := strings.TrimSpace(body.Raw); raw != "" {
		parsed, err := ParseRaw(raw)
		if err == nil {
			if msg.Subject == "" {
				msg.Subject = parsed.Subject
			}
			if msg.Sender == "" {
				msg.Sender = parsed.From
			}
			if msg.MessageID == "" {
				msg.MessageID = parsed.MessageID
			}
			if msg.Snippet == "" {
				msg.Snippet = truncate(strings.TrimSpace(parsed.Text), 300)
			}
			msg.TextBody = parsed.Text
			res := ExtractFromParsed(parsed, parsed.Text)
			msg.Code = res.Code
			msg.ExtractStatus = res.Status
			if msg.Link == "" {
				msg.Link = res.Link
			}
		} else if msg.ExtractStatus == "" {
			msg.ExtractStatus = ExtractNone
		}
	} else if msg.ExtractStatus == "" {
		if msg.Code != "" {
			msg.ExtractStatus = ExtractOK
		} else {
			msg.ExtractStatus = ExtractNone
		}
	}

	id, err := s.Ingest(r.Context(), msg)
	if err != nil {
		// 公开投递入口不回显内部错误细节，避免泄露存储信息。
		response.Error(w, http.StatusInternalServerError, "ingest 失败")
		return
	}
	response.OK(w, map[string]interface{}{"id": id, "status": msg.ExtractStatus})
}

// handleDomains 返回已部署收件箱、可用于生成收件邮箱的域名。
func (s *Service) handleDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	domains, err := s.AvailableDomains(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"domains": domains})
}

// handleMessages 查询收件箱列表。
// GET /api/emailcode/messages?mailbox=&includeConsumed=1&limit=50
func (s *Service) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	mailbox := strings.TrimSpace(r.URL.Query().Get("mailbox"))
	includeConsumed := r.URL.Query().Get("includeConsumed") == "1"
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	messages, err := s.List(r.Context(), mailbox, includeConsumed, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"messages": messages})
}

// handleStats 返回提取统计。
func (s *Service) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	stats, err := s.Stats(r.Context(), days)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, stats)
}

// handleClear 清理邮件：onlyConsumed=1 只删已消费的。
func (s *Service) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	onlyConsumed := r.URL.Query().Get("onlyConsumed") == "1"
	n, err := s.Clear(r.Context(), onlyConsumed)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"deleted": n})
}

// handleMessageItem 处理单条邮件的详情、取用与删除。
// GET    /api/emailcode/messages/{id}
// POST   /api/emailcode/messages/{id}/consume
// DELETE /api/emailcode/messages/{id}
func (s *Service) handleMessageItem(w http.ResponseWriter, r *http.Request, rest string) {
	rest = strings.Trim(rest, "/")
	if idx := strings.Index(rest, "/"); idx >= 0 {
		idPart, action := rest[:idx], rest[idx+1:]
		if action == "consume" && r.Method == http.MethodPost {
			s.consumeMessage(w, r, idPart)
			return
		}
		response.Error(w, http.StatusNotFound, "emailcode route not found")
		return
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		response.Error(w, http.StatusBadRequest, "无效的邮件 id")
		return
	}
	switch r.Method {
	case http.MethodGet:
		msg, err := s.Get(r.Context(), id)
		if err != nil {
			response.Error(w, http.StatusNotFound, "邮件不存在")
			return
		}
		response.OK(w, msg)
	case http.MethodDelete:
		if err := s.Delete(r.Context(), id); err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, map[string]interface{}{"id": id})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) consumeMessage(w http.ResponseWriter, r *http.Request, idPart string) {
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil || id <= 0 {
		response.Error(w, http.StatusBadRequest, "无效的邮件 id")
		return
	}
	if err := s.Consume(r.Context(), id, "manual"); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]interface{}{"id": id})
}

// handleWait 阻塞等待匹配选择器的验证码邮件，供注册/登录等消费者使用。
// POST /api/emailcode/wait {mailbox, domain?, fromDomain?, subjectContains?, since?, timeoutSec?, consumer?}
func (s *Service) handleWait(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Mailbox         string `json:"mailbox"`
		Domain          string `json:"domain"`
		FromDomain      string `json:"fromDomain"`
		SubjectContains string `json:"subjectContains"`
		Since           string `json:"since"`
		TimeoutSec      int    `json:"timeoutSec"`
		Consumer        string `json:"consumer"`
		RequireCode     *bool  `json:"requireCode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&body); err != nil {
		response.Error(w, http.StatusBadRequest, "请求体解析失败")
		return
	}
	mailbox := strings.TrimSpace(body.Mailbox)
	if mailbox == "" {
		response.Error(w, http.StatusBadRequest, "收件人不能为空")
		return
	}
	var since time.Time
	if body.Since != "" {
		if ts, err := time.Parse(time.RFC3339, body.Since); err == nil {
			since = ts
		}
	}
	requireCode := true
	if body.RequireCode != nil {
		requireCode = *body.RequireCode
	}
	sel := Selector{
		Mailbox:         mailbox,
		Domain:          strings.TrimSpace(body.Domain),
		FromDomain:      strings.TrimSpace(body.FromDomain),
		SubjectContains: strings.TrimSpace(body.SubjectContains),
		Since:           since,
		RequireCode:     requireCode,
	}
	consumer := strings.TrimSpace(body.Consumer)
	if consumer == "" {
		consumer = "wait"
	}
	// 等待上限封顶，避免持会话的用户用大 timeoutSec 长时间占用阻塞 handler 与等待队列。
	if body.TimeoutSec <= 0 {
		body.TimeoutSec = 300
	} else if body.TimeoutSec > 600 {
		body.TimeoutSec = 600
	}
	timeout := time.Duration(body.TimeoutSec) * time.Second
	msg, err := s.Wait(r.Context(), sel, consumer, timeout)
	if err != nil {
		response.Error(w, http.StatusGatewayTimeout, err.Error())
		return
	}
	response.OK(w, msg)
}

// verifyWorkerSecret 校验邮件 Worker 的共享密钥。
func verifyWorkerSecret(r *http.Request) bool {
	expected := WorkerSecret()
	got := strings.TrimSpace(r.Header.Get("X-Worker-Secret"))
	if got == "" || expected == "" || len(got) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}
