// Package nodes 是嵌入引擎的单节点桩。原版依赖 mihomo 节点池，嵌入版不携带该
// 重依赖：SelectForParallel 恒返回空，使竞速引擎（race_engine）自动降级为
// 「单节点直连」路径（proxy = ActiveNodeURI 或 ProxyURL，即模型网关代理池入口）。
package nodes

// Node 保留原版结构，仅承载 URI/名称展示字段。
type Node struct {
	RawURI string
	Name   string
}

// StickyPool 是原版粘性池的 no-op 桩。
type StickyPool struct{}

func (p *StickyPool) Add(uri string)   {}
func (p *StickyPool) Evict(uri string) {}

func GetStickyPool() *StickyPool { return &StickyPool{} }

// SelectForParallel 恒返回空候选，强制竞速引擎走单节点直连路径。
func SelectForParallel(size, topK int, debugMode, stickyPriority bool) []Node {
	return nil
}

func GetNodeName(uri string) string {
	if uri == "" {
		return "直连"
	}
	return uri
}

func RecordTest(uri string, ok bool, ms int, errMsg string) {}
func RecordRateLimit(uri string, seconds int)              {}
func IncInFlight(uri string)                               {}
func DecInFlight(uri string)                               {}
func GetAverageLatency() int                               { return 0 }
