package aiagent

import "context"

// aiInternalCallKey 是「管理 AI 内部调用」的 context key 类型。
// 只在服务端内部（server/ai_caller.go 处理 AI 调用时）对 aiagent 路由注入，
// 普通 HTTP 请求不可能携带该 context 值，因此不会伪造管理员身份。
type aiInternalCallKey struct{}

// WithInternalAICall 返回带「管理 AI 内部调用」标记的 context。
func WithInternalAICall(ctx context.Context) context.Context {
	return context.WithValue(ctx, aiInternalCallKey{}, true)
}

// isInternalAICall 判断请求上下文是否来自管理 AI 内部调用。
func isInternalAICall(ctx context.Context) bool {
	v, _ := ctx.Value(aiInternalCallKey{}).(bool)
	return v
}
