package proxypool

import "fmt"

// errSubHTTPStatus 是订阅拉取的 HTTP 状态错误。
type errSubHTTPStatus int

func (e errSubHTTPStatus) Error() string { return fmt.Sprintf("订阅源返回 HTTP %d", int(e)) }