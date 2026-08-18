// Package sseutil 提供 SSE 长连接 / 长任务响应的写超时续期工具。
//
// http.Server.WriteTimeout 是自请求开始起的绝对写截止时间，长连接每次写
// 都必须先续期，否则 60 秒后任何写入都会失败、事件被静默丢弃。
package sseutil

import (
	"errors"
	"net/http"
	"time"
)

// DefaultKeepAlive 为默认续期窗口；与现有续期流的取值保持一致。
const DefaultKeepAlive = 10 * time.Minute

// RenewWriteDeadline 在每次写入前延长响应写截止时间。
// 返回非 nil 错误表示连接已不可写，调用方应停止输出；
// 不支持的 ResponseWriter（如 httptest.Recorder）视为可继续写。
func RenewWriteDeadline(w http.ResponseWriter, keepAlive time.Duration) error {
	if keepAlive <= 0 {
		keepAlive = DefaultKeepAlive
	}
	err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(keepAlive))
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}