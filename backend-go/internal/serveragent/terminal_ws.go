package serveragent

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"golang.org/x/crypto/ssh"
)

type terminalWSMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

var sshTerminalUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Service) handleSSHTerminal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	serverID := strings.TrimSpace(firstNonEmptyString(
		r.URL.Query().Get("server_id"),
		r.URL.Query().Get("serverId"),
		r.URL.Query().Get("id"),
	))
	if serverID == "" {
		response.Error(w, http.StatusBadRequest, "server_id required")
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "database connection failed: "+err.Error())
		return
	}
	defer db.Close()

	cfg, err := s.getSFTPServerConfig(r, db, serverID)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := ensureTerminalAuthConfigured(cfg); err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	conn, err := sshTerminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	s.runSSHTerminalSession(r, conn, cfg)
}

func (s *Service) runSSHTerminalSession(r *http.Request, conn *websocket.Conn, cfg sftpServerConfig) {
	writeMu := &sync.Mutex{}
	writeJSON := func(msg terminalWSMessage) bool {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(msg) == nil
	}

	authMethods, err := terminalSSHAuthMethods(cfg)
	if err != nil {
		writeJSON(terminalWSMessage{Type: "error", Data: err.Error()})
		return
	}

	client, err := ssh.Dial("tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         20 * time.Second,
	})
	if err != nil {
		writeJSON(terminalWSMessage{Type: "error", Data: fmt.Sprintf("CONNECTION_FAILED: %v", err)})
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		writeJSON(terminalWSMessage{Type: "error", Data: fmt.Sprintf("SESSION_FAILED: %v", err)})
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
		writeJSON(terminalWSMessage{Type: "error", Data: fmt.Sprintf("PTY_FAILED: %v", err)})
		return
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		writeJSON(terminalWSMessage{Type: "error", Data: fmt.Sprintf("STDIN_FAILED: %v", err)})
		return
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		writeJSON(terminalWSMessage{Type: "error", Data: fmt.Sprintf("STDOUT_FAILED: %v", err)})
		return
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		writeJSON(terminalWSMessage{Type: "error", Data: fmt.Sprintf("STDERR_FAILED: %v", err)})
		return
	}

	if err := session.Shell(); err != nil {
		writeJSON(terminalWSMessage{Type: "error", Data: fmt.Sprintf("SHELL_FAILED: %v", err)})
		return
	}
	writeJSON(terminalWSMessage{Type: "status", Data: "connected"})

	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() { closeOnce.Do(func() { close(done) }) }

	copyOutput := func(reader io.Reader) {
		defer closeDone()
		buf := make([]byte, 4096)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				if !writeJSON(terminalWSMessage{Type: "data", Data: string(buf[:n])}) {
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
		var msg terminalWSMessage
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
		}
	}
}

func terminalSSHAuthMethods(cfg sftpServerConfig) ([]ssh.AuthMethod, error) {
	authMethods := []ssh.AuthMethod{}
	if cfg.AuthType == "key" {
		var signer ssh.Signer
		var err error
		if cfg.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(cfg.PrivateKey), []byte(cfg.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("AUTH_FAILED: SSH private key parse failed")
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	} else if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	}
	if len(authMethods) == 0 {
		return nil, fmt.Errorf("CONFIG_INCOMPLETE: SSH credentials are incomplete")
	}
	return authMethods, nil
}

func ensureTerminalAuthConfigured(cfg sftpServerConfig) error {
	if cfg.AuthType == "key" && cfg.PrivateKey == "" {
		return fmt.Errorf("CONFIG_INCOMPLETE: SSH private key is required")
	}
	if cfg.AuthType != "key" && cfg.Password == "" {
		return fmt.Errorf("CONFIG_INCOMPLETE: SSH password is required")
	}
	return nil
}

func intQuery(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
