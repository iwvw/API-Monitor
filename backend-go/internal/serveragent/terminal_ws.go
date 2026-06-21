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
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	Transport string `json:"transport,omitempty"`
}

const (
	terminalWriteWait    = 10 * time.Second
	terminalPongWait     = 75 * time.Second
	terminalPingInterval = 25 * time.Second
)

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

	transport := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("transport")))
	if transport == "" {
		transport = "auto"
	}
	_, agentOnline := s.registry.Get(serverID)

	conn, err := sshTerminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	if (transport == "auto" || transport == "agent") && agentOnline {
		s.runAgentTerminalSession(r, conn, serverID)
		return
	}
	if transport == "agent" {
		_ = conn.WriteJSON(terminalWSMessage{Type: "error", Data: "AGENT_OFFLINE: agent is not connected", Transport: "agent"})
		return
	}

	db, err := s.open(r.Context())
	if err != nil {
		_ = conn.WriteJSON(terminalWSMessage{Type: "error", Data: "database connection failed: " + err.Error(), Transport: "ssh"})
		return
	}
	defer db.Close()

	cfg, err := s.getSFTPServerConfig(r, db, serverID)
	if err != nil {
		_ = conn.WriteJSON(terminalWSMessage{Type: "error", Data: err.Error(), Transport: "ssh"})
		return
	}
	if err := ensureTerminalAuthConfigured(cfg); err != nil {
		_ = conn.WriteJSON(terminalWSMessage{Type: "error", Data: err.Error(), Transport: "ssh"})
		return
	}

	s.runSSHTerminalSession(r, conn, cfg)
}

func (s *Service) runAgentTerminalSession(r *http.Request, conn *websocket.Conn, serverID string) {
	writeMu := &sync.Mutex{}
	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() { closeOnce.Do(func() { close(done) }) }
	defer closeDone()
	configureTerminalWebSocket(conn)
	go startTerminalWebSocketHeartbeat(conn, writeMu, done)

	writeJSON := func(msg terminalWSMessage) bool {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(terminalWriteWait))
		return conn.WriteJSON(msg) == nil
	}

	agentConn, ok := s.registry.Get(serverID)
	if !ok {
		writeJSON(terminalWSMessage{Type: "error", Data: "AGENT_OFFLINE: agent is not connected", Transport: "agent"})
		return
	}

	ptyID := strings.TrimSpace(firstNonEmptyString(
		r.URL.Query().Get("pty_id"),
		r.URL.Query().Get("ptyId"),
		r.URL.Query().Get("session_id"),
		r.URL.Query().Get("sessionId"),
	))
	if ptyID == "" {
		ptyID = fmt.Sprintf("pty-%d", time.Now().UnixNano())
	}
	attachOnly := r.URL.Query().Get("attach") == "1" || strings.EqualFold(r.URL.Query().Get("attach"), "true")

	cols := intQuery(r, "cols", 120)
	rows := intQuery(r, "rows", 32)
	containerName := strings.TrimSpace(r.URL.Query().Get("container"))
	if s.ptyHub == nil {
		s.ptyHub = newPtyDataHub()
	}
	dataCh, cancel := s.ptyHub.Subscribe(ptyID)
	defer cancel()
	statusCh, cancelStatus := s.ptyHub.Subscribe("status:" + ptyID)
	defer cancelStatus()

	statusConfirmed := false
	if !attachOnly {
		startPayload := map[string]interface{}{"cols": cols, "rows": rows}
		if containerName != "" {
			startPayload["command"] = "docker"
			startPayload["args"] = []string{
				"exec",
				"-it",
				containerName,
				"sh",
				"-lc",
				"exec /bin/bash || exec /bin/sh || exec sh",
			}
		}
		dataBytes, _ := json.Marshal(startPayload)
		if err := agentConn.SendEvent("dashboard:task", map[string]interface{}{
			"id":      ptyID,
			"type":    12,
			"data":    string(dataBytes),
			"timeout": 0,
		}); err != nil {
			writeJSON(terminalWSMessage{Type: "error", Data: "AGENT_PTY_START_FAILED: " + err.Error(), Transport: "agent"})
			return
		}
		defer func() {
			_ = agentConn.SendEvent("dashboard:pty_stop", map[string]interface{}{
				"id": ptyID,
			})
		}()

		select {
		case rawStatus := <-statusCh:
			var status struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Error  string `json:"error"`
			}
			if err := json.Unmarshal([]byte(rawStatus), &status); err == nil {
				if strings.EqualFold(status.Status, "error") {
					msg := strings.TrimSpace(status.Error)
					if msg == "" {
						msg = "agent PTY start failed"
					}
					writeJSON(terminalWSMessage{Type: "error", Data: "AGENT_PTY_START_FAILED: " + msg, Transport: "agent"})
					return
				}
				if strings.EqualFold(status.Status, "ready") {
					statusConfirmed = true
				}
			}
		case <-time.After(3 * time.Second):
			// Older agents do not emit PTY status; keep compatibility during rolling upgrades.
		}
	}

	go func() {
		defer closeDone()
		for data := range dataCh {
			if !writeJSON(terminalWSMessage{Type: "data", Data: data, Transport: "agent"}) {
				return
			}
		}
	}()

	if attachOnly {
		writeJSON(terminalWSMessage{Type: "status", Data: "attached", Transport: "agent"})
	} else if statusConfirmed {
		writeJSON(terminalWSMessage{Type: "status", Data: "connected", Transport: "agent"})
	} else {
		writeJSON(terminalWSMessage{Type: "status", Data: "connected_legacy", Transport: "agent"})
	}

	if containerName != "" && !statusConfirmed {
		execCmd := dockerExecShellCommand(containerName)
		_ = agentConn.SendEvent("dashboard:pty_input", map[string]interface{}{
			"id":   ptyID,
			"data": execCmd,
		})
	}

	for {
		select {
		case <-done:
			return
		default:
		}

		_, payload, err := conn.ReadMessage()
		if err != nil {
			closeDone()
			return
		}
		var msg terminalWSMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "input":
			if msg.Data != "" {
				_ = agentConn.SendEvent("dashboard:pty_input", map[string]interface{}{
					"id":   ptyID,
					"data": msg.Data,
				})
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				_ = agentConn.SendEvent("dashboard:pty_resize", map[string]interface{}{
					"id":   ptyID,
					"cols": msg.Cols,
					"rows": msg.Rows,
				})
			}
		case "disconnect":
			closeDone()
			return
		}
	}
}

func (s *Service) runSSHTerminalSession(r *http.Request, conn *websocket.Conn, cfg sftpServerConfig) {
	writeMu := &sync.Mutex{}
	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() { closeOnce.Do(func() { close(done) }) }
	defer closeDone()
	configureTerminalWebSocket(conn)
	go startTerminalWebSocketHeartbeat(conn, writeMu, done)

	writeJSON := func(msg terminalWSMessage) bool {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(terminalWriteWait))
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
	writeJSON(terminalWSMessage{Type: "status", Data: "connected", Transport: "ssh"})

	containerName := strings.TrimSpace(r.URL.Query().Get("container"))
	if containerName != "" {
		execCmd := dockerExecShellCommand(containerName)
		_, _ = stdin.Write([]byte(execCmd))
	}

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
		case "disconnect":
			closeDone()
			return
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

func configureTerminalWebSocket(conn *websocket.Conn) {
	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(terminalPongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(terminalPongWait))
		return nil
	})
}

func startTerminalWebSocketHeartbeat(conn *websocket.Conn, writeMu *sync.Mutex, done <-chan struct{}) {
	ticker := time.NewTicker(terminalPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			writeMu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(terminalWriteWait))
			err := conn.WriteMessage(websocket.PingMessage, nil)
			writeMu.Unlock()
			if err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}

func dockerExecShellCommand(container string) string {
	quotedContainer := shellQuote(container)
	quotedScript := shellQuote("exec /bin/bash || exec /bin/sh || exec sh")
	return fmt.Sprintf("docker exec -it %s sh -lc %s\n", quotedContainer, quotedScript)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
