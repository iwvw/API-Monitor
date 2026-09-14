package serveragent

import (
	"encoding/json"
	"fmt"
	"time"
)

func (s *Service) runAgentTaskAndWait(serverID string, taskType int, command string, timeout time.Duration) (string, error) {
	return s.runAgentTaskAndWaitMode(serverID, taskType, command, timeout, false)
}

func (s *Service) runAgentTaskAndWaitTransient(serverID string, taskType int, command string, timeout time.Duration) (string, error) {
	return s.runAgentTaskAndWaitMode(serverID, taskType, command, timeout, true)
}

func (s *Service) runAgentTaskAndWaitMode(serverID string, taskType int, command string, timeout time.Duration, transient bool) (string, error) {
	conn, ok := s.registry.Get(serverID)
	if !ok {
		return "", fmt.Errorf("agent offline")
	}

	taskTypeName := fmt.Sprintf("docker.internal.%d", taskType)
	if taskType == 51 {
		taskTypeName = "proxy.cloudflared"
	} else if taskType == 50 {
		taskTypeName = "proxy.runtime"
	}
	displayCommand := command
	if taskType == 50 || taskType == 51 {
		displayCommand = "structured managed-runtime payload"
	}
	var task *Task
	if transient {
		task = s.taskRegistry.CreateTransient(serverID, taskTypeName, displayCommand)
	} else {
		task = s.taskRegistry.Create(serverID, taskTypeName, displayCommand)
	}
	eventCh := task.Subscribe()

	err := conn.SendEvent("dashboard:task", map[string]interface{}{
		"id":      task.ID,
		"type":    taskType,
		"data":    command,
		"timeout": int(timeout.Seconds()),
	})
	if err != nil {
		s.taskRegistry.Fail(task.ID, err.Error())
		return "", err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				return "", fmt.Errorf("task channel closed")
			}
			if event.Status == TaskCompleted {
				if str, ok := event.Data.(string); ok {
					return str, nil
				}
				return fmt.Sprintf("%v", event.Data), nil
			}
			if event.Status == TaskFailed {
				return "", fmt.Errorf("%s", event.Error)
			}
		case <-timer.C:
			s.taskRegistry.Fail(task.ID, "task timeout")
			return "", fmt.Errorf("task timeout")
		}
	}
}

func (s *Service) RunCommandTaskAndWait(serverID string, command string, timeout time.Duration) (string, error) {
	return s.runAgentTaskAndWait(serverID, 1, command, timeout)
}

// RunProxyRuntimeTaskAndWait exposes a structured, non-shell task seam for the
// managed proxy module. Callers cannot vary the task type or timeout.
func (s *Service) RunProxyRuntimeTaskAndWait(serverID, desiredState string) (string, error) {
	return s.runAgentTaskAndWait(serverID, 50, desiredState, 3*time.Minute)
}

func (s *Service) runProxyRuntimeProbeAndWait(serverID, desiredState string) (string, error) {
	return s.runAgentTaskAndWaitMode(serverID, 50, desiredState, 30*time.Second, true)
}

func (s *Service) RunCloudflaredTaskAndWait(serverID, desiredState string) (string, error) {
	return s.runAgentTaskAndWait(serverID, 51, desiredState, 3*time.Minute)
}

// RunTCPForwarderTaskAndWait sends a tcp_forwarder reconcile task (task 53) and waits.
func (s *Service) RunTCPForwarderTaskAndWait(serverID, payload string) (string, error) {
	return s.runAgentTaskAndWait(serverID, 53, payload, 3*time.Minute)
}

// RunP2PTaskAndWait sends a p2p reconcile task (task 54: nat/p2p 打洞) and waits.
func (s *Service) RunP2PTaskAndWait(serverID, payload string) (string, error) {
	return s.runAgentTaskAndWait(serverID, 54, payload, 60*time.Second)
}

// RunTCPForwarderBootstrap 让目标主机的 Agent 安装并常驻 api-monitor-relay（幂等）。
func (s *Service) RunTCPForwarderBootstrap(serverID, assetURL, assetSHA string) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"operation": "bootstrap_relay", "relay_asset_url": assetURL, "relay_asset_sha256": assetSHA,
	})
	_, err := s.runAgentTaskAndWait(serverID, 53, string(payload), 3*time.Minute)
	return err
}

// RunTCPForwarderStatus 查询源主机 Agent 隧道状态，返回（活跃连接数, 隧道是否在线）。
func (s *Service) RunTCPForwarderStatus(serverID, forwardID string) (int, bool) {
	payload, _ := json.Marshal(map[string]interface{}{"operation": "status", "forward_id": forwardID})
	out, err := s.runAgentTaskAndWaitTransient(serverID, 53, string(payload), 6*time.Second)
	if err != nil {
		return 0, false
	}
	var st struct {
		Connected      bool `json:"connected"`
		ConnectorCount int  `json:"connector_count"`
	}
	if json.Unmarshal([]byte(out), &st) != nil {
		return 0, false
	}
	return st.ConnectorCount, st.Connected
}

// RunForwardHealthProbeAndWait asks a server's Agent to TCP-probe a host:port (task 40),
// returning true when the target is reachable. Forward health checks must probe from the
// target server's Agent — dialing from the panel process would hit the wrong machine.
func (s *Service) RunForwardHealthProbeAndWait(serverID, host string, port, timeoutSeconds int) bool {
	timeoutMS := timeoutSeconds * 1000
	if timeoutMS < 200 {
		timeoutMS = 200
	}
	if timeoutMS > 10000 {
		timeoutMS = 10000
	}
	payload := fmt.Sprintf(`{"targets":[{"id":null,"name":"forward-health","host":%q,"port":%d,"type":"tcp"}],"timeout_ms":%d}`, host, port, timeoutMS)
	out, err := s.runAgentTaskAndWaitTransient(serverID, 40, payload, 15*time.Second)
	if err != nil {
		return false
	}
	var resp struct {
		Results []struct {
			Success bool `json:"success"`
		} `json:"results"`
	}
	if json.Unmarshal([]byte(out), &resp) != nil || len(resp.Results) == 0 {
		return false
	}
	return resp.Results[0].Success
}

// RunAgentSelfUninstallTaskAndWait schedules removal from a detached helper
// owned by the Agent. The helper acknowledges before stopping the Agent
// service, so the control-plane task can complete deterministically.
func (s *Service) RunAgentSelfUninstallTaskAndWait(serverID string) (string, error) {
	return s.runAgentTaskAndWait(serverID, 52, "{}", 30*time.Second)
}

func (s *Service) hasAgentConnection(serverID string) bool {
	_, ok := s.registry.Get(serverID)
	return ok
}

func (s *Service) runAgentFileTask(serverID string, taskType int, payload map[string]interface{}, timeout time.Duration) (string, error) {
	return s.runAgentFileTaskMode(serverID, taskType, payload, timeout, false)
}

func (s *Service) runAgentTransientFileTask(serverID string, taskType int, payload map[string]interface{}, timeout time.Duration) (string, error) {
	return s.runAgentFileTaskMode(serverID, taskType, payload, timeout, true)
}

func (s *Service) runAgentFileTaskMode(serverID string, taskType int, payload map[string]interface{}, timeout time.Duration, transient bool) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("AGENT_FILE_ERROR: %w", err)
	}
	var result string
	if transient {
		result, err = s.runAgentTaskAndWaitTransient(serverID, taskType, string(data), timeout)
	} else {
		result, err = s.runAgentTaskAndWait(serverID, taskType, string(data), timeout)
	}
	if err != nil {
		return "", fmt.Errorf("AGENT_FILE_ERROR: %w", err)
	}
	return result, nil
}
