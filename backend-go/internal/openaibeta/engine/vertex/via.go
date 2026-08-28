package vertex

import (
	"context"
	"sync"
)

// 嵌入版新增的显式代理入口：中继层按请求从模型网关代理池选好出口代理后，
// 直接走单候选执行（等同竞速引擎「单节点直连」路径），避免把共享 ConfigProvider
// 当作可变代理槽（并发安全）。

// CompleteChatViaProxy 单候选非流式完成：proxyURI 为空表示直连。
func (c *VertexAIClient) CompleteChatViaProxy(ctx context.Context, model string, geminiPayload map[string]any, proxyURI string) (map[string]any, error) {
	return c.runSingleCandidate(ctx, model, geminiPayload, proxyURI)
}

// CompleteChatNViaProxy 并行 n 路单候选完成，收集成功结果（与上游 CompleteChatN 一致）。
func (c *VertexAIClient) CompleteChatNViaProxy(ctx context.Context, model string, geminiPayload map[string]any, n int, proxyURI string) ([]map[string]any, error) {
	type res struct {
		resp map[string]any
		err  error
	}
	results := make([]res, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			r, err := c.CompleteChatViaProxy(ctx, model, geminiPayload, proxyURI)
			results[idx] = res{resp: r, err: err}
		}(i)
	}
	wg.Wait()

	var ok []map[string]any
	var firstErr error
	for _, r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		ok = append(ok, r.resp)
	}
	if len(ok) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, NewEmptyResponseError("all n candidates returned no data")
	}
	return ok, nil
}

// StreamChatViaProxy 单候选流式：proxyURI 为空表示直连。
func (c *VertexAIClient) StreamChatViaProxy(ctx context.Context, model string, geminiPayload map[string]any, proxyURI string, yield func(StreamChunk) bool) {
	c.executeStreamingWithRetries(ctx, model, geminiPayload, proxyURI, yield)
}
