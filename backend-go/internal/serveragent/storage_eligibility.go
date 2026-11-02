package serveragent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
)

const DefaultStoragePort = 61208

// StorageNodeInfo 表示可用作文件柜存储/分发目标的节点信息
type StorageNodeInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	StoragePort int    `json:"storagePort"`
	Platform    string `json:"platform"`
	Online      bool   `json:"online"`
}

// IsStorageNodeEligible 统一判定节点是否可作存储/分发目标：
// 约束：必须同时满足 在线 + 非 Windows + 公网可达。
func IsStorageNodeEligible(status, host, platform string) bool {
	// 1. 必须在线
	if !strings.EqualFold(strings.TrimSpace(status), "online") {
		return false
	}

	// 2. 必须非 Windows
	plat := strings.ToLower(strings.TrimSpace(platform))
	if strings.Contains(plat, "windows") || plat == "win32" || plat == "win64" {
		return false
	}

	// 3. 必须具有公网可达地址
	if !IsHostPublic(host) {
		return false
	}

	return true
}

// IsHostPublic 判断给定 host 字符串是否为公网可达地址
func IsHostPublic(hostStr string) bool {
	h := strings.TrimSpace(hostStr)
	// 剥离可能存在的端口号
	if strings.Contains(h, ":") && !strings.HasPrefix(h, "[") {
		if hostPart, _, err := net.SplitHostPort(h); err == nil {
			h = hostPart
		}
	} else if strings.HasPrefix(h, "[") && strings.Contains(h, "]:") {
		if hostPart, _, err := net.SplitHostPort(h); err == nil {
			h = strings.Trim(hostPart, "[]")
		}
	}
	h = strings.Trim(h, "[]")

	if h == "" || h == "0.0.0.0" || h == "::" {
		return false
	}

	// 检查特殊/内网主机名
	lower := strings.ToLower(h)
	if lower == "localhost" ||
		strings.HasSuffix(lower, ".localhost") ||
		strings.HasSuffix(lower, ".local") ||
		strings.HasSuffix(lower, ".internal") ||
		strings.HasSuffix(lower, ".lan") ||
		strings.HasSuffix(lower, ".home.arpa") {
		return false
	}

	// 尝试作为 IP 解析
	parsedIP := net.ParseIP(h)
	if parsedIP != nil {
		return IsIPPublic(parsedIP)
	}

	// 若为普通域名且无私有后缀，视为公网域名
	//（若包含点且首尾字符合法）
	if strings.Contains(h, ".") && !strings.HasPrefix(h, ".") && !strings.HasSuffix(h, ".") {
		return true
	}

	return false
}

// IsIPPublic 判断 IP 地址是否为公网单播地址（非私有、非回环、非链路本地、非多播、非保留）
func IsIPPublic(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsUnspecified() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		!ip.IsGlobalUnicast() {
		return false
	}
	return true
}

// ListEligibleStorageNodes 查询所有满足 #46 约束的可用存储节点列表
func (s *Service) ListEligibleStorageNodes(ctx context.Context) ([]StorageNodeInfo, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `
		SELECT id, name, host, status, cached_info
		FROM server_accounts
		WHERE status = 'online'
		ORDER BY order_index ASC, name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query storage nodes: %w", err)
	}
	defer rows.Close()

	var nodes []StorageNodeInfo
	for rows.Next() {
		var id, name, host, status string
		var cachedInfo sql.NullString
		if err := rows.Scan(&id, &name, &host, &status, &cachedInfo); err != nil {
			return nil, err
		}

		// 结合实时连接与缓存解析 platform 与公网 host
		platform, effectiveHost, storagePort := s.resolveNodeLiveDetails(id, host, cachedInfo.String)

		// 检查在线状态：优先从活跃长连验证
		isLiveOnline := s.isNodeLiveConnected(id, status)
		if !isLiveOnline {
			continue
		}

		if !IsStorageNodeEligible("online", effectiveHost, platform) {
			continue
		}

		nodes = append(nodes, StorageNodeInfo{
			ID:          id,
			Name:        name,
			Host:        effectiveHost,
			StoragePort: storagePort,
			Platform:    platform,
			Online:      true,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return nodes, nil
}

// GetEligibleStorageNode 获取指定可用存储节点，如果不满足 #46 则返回错误
func (s *Service) GetEligibleStorageNode(ctx context.Context, serverID string) (*StorageNodeInfo, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var name, host, status string
	var cachedInfo sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT name, host, status, cached_info
		FROM server_accounts
		WHERE id = ?
	`, serverID).Scan(&name, &host, &status, &cachedInfo)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("节点不存在: %s", serverID)
		}
		return nil, err
	}

	platform, effectiveHost, storagePort := s.resolveNodeLiveDetails(serverID, host, cachedInfo.String)
	isLiveOnline := s.isNodeLiveConnected(serverID, status)
	if !isLiveOnline {
		return nil, fmt.Errorf("节点当前离线: %s", serverID)
	}

	if !IsStorageNodeEligible("online", effectiveHost, platform) {
		platLower := strings.ToLower(platform)
		if strings.Contains(platLower, "windows") {
			return nil, fmt.Errorf("Windows 节点不支持作为存储目标: %s", serverID)
		}
		if !IsHostPublic(effectiveHost) {
			return nil, fmt.Errorf("节点无公网可达地址: %s (%s)", serverID, effectiveHost)
		}
		return nil, fmt.Errorf("节点不可用作存储目标: %s", serverID)
	}

	return &StorageNodeInfo{
		ID:          serverID,
		Name:        name,
		Host:        effectiveHost,
		StoragePort: storagePort,
		Platform:    platform,
		Online:      true,
	}, nil
}

// GetStorageNodeAgentKey 获取目标节点的通信密钥（用于计算 HMAC 签名）
func (s *Service) GetStorageNodeAgentKey(ctx context.Context, serverID string) (string, error) {
	db, err := s.open(ctx)
	if err != nil {
		return "", err
	}
	defer db.Close()

	return s.getOrGenerateAgentKeyForServer(ctx, db, serverID)
}

func (s *Service) isNodeLiveConnected(serverID, dbStatus string) bool {
	if s.registry != nil {
		if _, ok := s.registry.Get(serverID); ok {
			return true
		}
	}
	return strings.EqualFold(dbStatus, "online")
}

func (s *Service) resolveNodeLiveDetails(serverID, dbHost, cachedInfoStr string) (platform, host string, port int) {
	port = DefaultStoragePort
	host = dbHost

	// 优先从实时会话中提取
	if s.registry != nil {
		if conn, ok := s.registry.Get(serverID); ok {
			conn.mu.RLock()
			meta := conn.Metadata
			conn.mu.RUnlock()
			if meta != nil {
				if p, ok := meta["platform"].(string); ok && p != "" {
					platform = p
				}
				if sp, ok := meta["storage_port"].(float64); ok && sp > 0 {
					port = int(sp)
				}
				if liveIP := firstNonEmpty(getString(meta, "public_ip"), getString(meta, "ip")); liveIP != "" && IsHostPublic(liveIP) {
					host = liveIP
				}
			}
		}
	}

	// 补充从 cached_info 提取
	if cachedInfoStr != "" {
		var info map[string]interface{}
		if err := json.Unmarshal([]byte(cachedInfoStr), &info); err == nil {
			if platform == "" {
				if p, ok := info["platform"].(string); ok {
					platform = p
				}
			}
			if !IsHostPublic(host) {
				if pip, ok := info["ip"].(string); ok && IsHostPublic(pip) {
					host = pip
				}
			}
		}
	}

	return platform, host, port
}
