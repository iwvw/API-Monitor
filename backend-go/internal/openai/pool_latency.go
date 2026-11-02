package openai

import (
	"crypto/rand"
	"math/big"
)

// endpointPickOverride 是测试专用确定性选路钩子（生产恒为 nil）：
// 覆盖延迟加权随机，供依赖「端点 A 先被选中」的 failover 测试消除 flake。
var endpointPickOverride func(candidates []Endpoint) int

// recordEndpointLatency 记录端点最近一次转发延迟（毫秒），供延迟加权分流使用。
func (s *Service) recordEndpointLatency(endpointID string, latencyMs int64) {
	if endpointID == "" || latencyMs <= 0 {
		return
	}
	s.latencyMu.Lock()
	s.endpointLatency[endpointID] = latencyMs
	s.endpointLatencyOK[endpointID] = true
	s.latencyMu.Unlock()
}

// getEndpointLatency 读取端点最近转发延迟；无记录时返回 (0, false)。
func (s *Service) getEndpointLatency(endpointID string) (int64, bool) {
	s.latencyMu.RLock()
	defer s.latencyMu.RUnlock()
	ok := s.endpointLatencyOK[endpointID]
	return s.endpointLatency[endpointID], ok
}

// weightedEndpointPick 在可服务同一模型的端点中按延迟加权随机选择：
// 权重 = 1 + (maxLatency - latency) / 200，延迟越低的端点权重越高，
// 健康快的端点被选中概率更高；尚无延迟记录的端点按中等延迟（maxLatency）
// 参与，保证首次使用也有机会被选中。返回选中下标。
func weightedEndpointPick(latencies []int64, known []bool) int {
	maxLatency := int64(0)
	for i, latency := range latencies {
		if known[i] && latency > maxLatency {
			maxLatency = latency
		}
	}
	if maxLatency == 0 {
		maxLatency = 1000
	}

	total := int64(0)
	weights := make([]int64, len(latencies))
	for i, latency := range latencies {
		effective := latency
		if !known[i] {
			// 无记录端点视为中等延迟，避免被饿死。
			effective = maxLatency
		}
		weight := int64(1) + (maxLatency-effective)/200
		if weight < 1 {
			weight = 1
		}
		weights[i] = weight
		total += weight
	}
	if total <= 0 {
		return 0
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(total))
	acc := int64(0)
	for i, w := range weights {
		acc += w
		if n.Int64() < acc {
			return i
		}
	}
	return len(latencies) - 1
}

// randIntN 返回 [0, n) 内的安全随机整数；n <= 0 时返回 0。
// 用于并发请求需要打散到不同出口的场景（如全池解冻后分散起点）。
func randIntN(n int) int {
	if n <= 0 {
		return 0
	}
	bigN, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(bigN.Int64())
}
