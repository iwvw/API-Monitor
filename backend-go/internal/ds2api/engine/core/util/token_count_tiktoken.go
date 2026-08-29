//go:build !386 && !arm && !mips && !mipsle && !wasm

package util

import (
	"strings"
	"sync"

	tiktoken "github.com/hupe1980/go-tiktoken"
)

var (
	tokenEncodingPools       sync.Map
	tokenEncodingUnsupported sync.Map
)

func countWithTokenizer(text, model string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	encoding, release := tokenizerEncodingForCount(tokenizerModelForCount(model))
	if encoding == nil {
		return 0
	}
	defer release()
	ids, _, err := encoding.Encode(text, nil, nil)
	if err != nil {
		return 0
	}
	return len(ids)
}

func tokenizerEncodingForCount(model string) (*tiktoken.Encoding, func()) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultTokenizerModel
	}
	if _, ok := tokenEncodingUnsupported.Load(model); ok {
		return nil, func() {}
	}
	if rawPool, ok := tokenEncodingPools.Load(model); ok {
		pool, _ := rawPool.(*sync.Pool)
		return getEncodingFromPool(pool)
	}

	encoding, err := tiktoken.NewEncodingForModel(model)
	if err != nil {
		tokenEncodingUnsupported.Store(model, struct{}{})
		return nil, func() {}
	}
	pool := &sync.Pool{
		New: func() any {
			encoding, err := tiktoken.NewEncodingForModel(model)
			if err != nil {
				return nil
			}
			return encoding
		},
	}
	actualPool, _ := tokenEncodingPools.LoadOrStore(model, pool)
	pool, _ = actualPool.(*sync.Pool)
	return encoding, func() {
		pool.Put(encoding)
	}
}

func getEncodingFromPool(pool *sync.Pool) (*tiktoken.Encoding, func()) {
	if pool == nil {
		return nil, func() {}
	}
	encoding, _ := pool.Get().(*tiktoken.Encoding)
	if encoding == nil {
		return nil, func() {}
	}
	return encoding, func() {
		pool.Put(encoding)
	}
}

func tokenizerModelForCount(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return defaultTokenizerModel
	}
	switch {
	case strings.HasPrefix(model, "claude"):
		return claudeTokenizerModel
	case strings.HasPrefix(model, "gpt-4"), strings.HasPrefix(model, "gpt-5"), strings.HasPrefix(model, "o1"), strings.HasPrefix(model, "o3"), strings.HasPrefix(model, "o4"):
		return defaultTokenizerModel
	case strings.HasPrefix(model, "deepseek-v4"):
		return defaultTokenizerModel
	case strings.HasPrefix(model, "deepseek"):
		return defaultTokenizerModel
	default:
		return defaultTokenizerModel
	}
}
