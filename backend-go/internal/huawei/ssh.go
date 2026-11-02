package huawei

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"golang.org/x/crypto/ssh"
)

// sshWSMessage WebSocket 终端消息协议（与 serveragent 终端一致）：
// 客户端→服务端：input / resize / disconnect；服务端→客户端：data / status / error。
type sshWSMessage struct {
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	Transport string `json:"transport,omitempty"`
}

const (
	sshWriteWait    = 10 * time.Second
	sshPongWait     = 75 * time.Second
	sshPingInterval = 25 * time.Second
)

func (s *Service) sshUpgrader() *websocket.Upgrader {
	return &websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin == "" {
				return true
			}
			parsed, err := url.Parse(origin)
			return err == nil && strings.EqualFold(parsed.Host, r.Host)
		},
	}
}

// sshTerminal 建立 WebSocket → SSH 桥接，直连华为云实例公网 IP。
// query: host（必填，实例公网 IP）、instanceId（可选，日志）、user/port（可选覆盖账号默认）。
func (s *Service) sshTerminal(w http.ResponseWriter, r *http.Request, accountID string) {
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	if host == "" {
		response.Error(w, http.StatusBadRequest, "host required")
		return
	}
	user := strings.TrimSpace(r.URL.Query().Get("user"))
	port := intQuery(r, "port", 0)

	conn, err := s.sshUpgrader().Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	account, db, ok := s.accountForRequestAfterUpgrade(r, conn, accountID)
	if !ok {
		return
	}
	defer db.Close()

	if user == "" {
		user = account.SSHUser
	}
	if user == "" {
		user = "root"
	}
	if port <= 0 {
		port = sshPortOrDefault(account.SSHPort)
	}

	authMethods, err := sshAuthMethods(account)
	if err != nil {
		_ = conn.WriteJSON(sshWSMessage{Type: "error", Data: err.Error(), Transport: "ssh"})
		return
	}

	client, err := ssh.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)), &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         20 * time.Second,
	})
	if err != nil {
		_ = conn.WriteJSON(sshWSMessage{Type: "error", Data: fmt.Sprintf("CONNECTION_FAILED: %v", err), Transport: "ssh"})
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		_ = conn.WriteJSON(sshWSMessage{Type: "error", Data: fmt.Sprintf("SESSION_FAILED: %v", err), Transport: "ssh"})
		return
	}
	defer session.Close()

	cols := intQuery(r, "cols", 120)
	rows := intQuery(r, "rows", 32)
	if err := session.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		_ = conn.WriteJSON(sshWSMessage{Type: "error", Data: fmt.Sprintf("PTY_FAILED: %v", err), Transport: "ssh"})
		return
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		_ = conn.WriteJSON(sshWSMessage{Type: "error", Data: fmt.Sprintf("STDIN_FAILED: %v", err), Transport: "ssh"})
		return
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = conn.WriteJSON(sshWSMessage{Type: "error", Data: fmt.Sprintf("STDOUT_FAILED: %v", err), Transport: "ssh"})
		return
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		_ = conn.WriteJSON(sshWSMessage{Type: "error", Data: fmt.Sprintf("STDERR_FAILED: %v", err), Transport: "ssh"})
		return
	}

	if err := session.Shell(); err != nil {
		_ = conn.WriteJSON(sshWSMessage{Type: "error", Data: fmt.Sprintf("SHELL_FAILED: %v", err), Transport: "ssh"})
		return
	}

	writeMu := &sync.Mutex{}
	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() { closeOnce.Do(func() { close(done) }) }
	defer closeDone()

	configureSSHWebSocket(conn)
	go startSSHWebSocketHeartbeat(conn, writeMu, done)

	writeJSON := func(msg sshWSMessage) bool {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(sshWriteWait))
		return conn.WriteJSON(msg) == nil
	}

	writeJSON(sshWSMessage{Type: "status", Data: "connected", Transport: "ssh"})

	copyOutput := func(reader io.Reader) {
		defer closeDone()
		buf := make([]byte, 4096)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				if !writeJSON(sshWSMessage{Type: "data", Data: string(buf[:n]), Transport: "ssh"}) {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	go copyOutput(stdout)
	go copyOutput(stderr)

	go func() {
		_ = session.Wait()
		closeDone()
	}()

	for {
		select {
		case <-done:
			return
		default:
		}

		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg sshWSMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "input":
			if msg.Data != "" {
				_, _ = io.WriteString(stdin, msg.Data)
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				_ = session.WindowChange(msg.Rows, msg.Cols)
			}
		case "disconnect":
			closeDone()
			return
		}
	}
}

// accountForRequestAfterUpgrade WebSocket 握手后读取账号（错误经 WS 通道返回）。
func (s *Service) accountForRequestAfterUpgrade(r *http.Request, conn *websocket.Conn, accountID string) (Account, *sql.DB, bool) {
	id, err := parseID(accountID)
	if err != nil {
		_ = conn.WriteJSON(sshWSMessage{Type: "error", Data: "invalid account id", Transport: "ssh"})
		return Account{}, nil, false
	}
	db, err := s.open(r.Context())
	if err != nil {
		_ = conn.WriteJSON(sshWSMessage{Type: "error", Data: "database connection failed: " + err.Error(), Transport: "ssh"})
		return Account{}, nil, false
	}
	account, err := getAccount(r.Context(), db, id)
	if err != nil {
		_ = conn.WriteJSON(sshWSMessage{Type: "error", Data: "account not found", Transport: "ssh"})
		return Account{}, nil, false
	}
	return account, db, true
}

// sshAuthMethods 依据账号 SSH 凭据构造认证：私钥优先，其次密码。
func sshAuthMethods(account Account) ([]ssh.AuthMethod, error) {
	authMethods := []ssh.AuthMethod{}
	if strings.TrimSpace(account.SSHPrivateKey) != "" {
		signer, err := ssh.ParsePrivateKey([]byte(account.SSHPrivateKey))
		if err != nil {
			return nil, fmt.Errorf("AUTH_FAILED: SSH private key parse failed")
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	if account.SSHPassword != "" {
		authMethods = append(authMethods, ssh.Password(account.SSHPassword))
	}
	if len(authMethods) == 0 {
		return nil, fmt.Errorf("CONFIG_INCOMPLETE: 账号未配置 SSH 私钥或密码，请在账号管理中补充")
	}
	return authMethods, nil
}

func configureSSHWebSocket(conn *websocket.Conn) {
	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(sshPongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(sshPongWait))
		return nil
	})
}

func startSSHWebSocketHeartbeat(conn *websocket.Conn, writeMu *sync.Mutex, done <-chan struct{}) {
	ticker := time.NewTicker(sshPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			writeMu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(sshWriteWait))
			err := conn.WriteMessage(websocket.PingMessage, nil)
			writeMu.Unlock()
			if err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}

func intQuery(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
