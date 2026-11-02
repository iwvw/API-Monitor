//go:build !386 && !arm && !mips && !mipsle && !wasm

package util

import "testing"

func TestTokenizerEncodingForCountCachesSupportedModel(t *testing.T) {
	encoding, release := tokenizerEncodingForCount(defaultTokenizerModel)
	if encoding == nil {
		t.Fatalf("expected tokenizer encoding for %q", defaultTokenizerModel)
	}
	release()

	if _, ok := tokenEncodingPools.Load(defaultTokenizerModel); !ok {
		t.Fatalf("expected tokenizer encoding pool for %q", defaultTokenizerModel)
	}

	encoding, release = tokenizerEncodingForCount(defaultTokenizerModel)
	if encoding == nil {
		t.Fatalf("expected cached tokenizer encoding for %q", defaultTokenizerModel)
	}
	release()
}

func TestTokenizerEncodingForCountCachesUnsupportedModel(t *testing.T) {
	const model = "__ds2api_unsupported_tokenizer_model__"
	encoding, release := tokenizerEncodingForCount(model)
	release()
	if encoding != nil {
		t.Fatalf("expected nil encoding for unsupported model %q", model)
	}
	if _, ok := tokenEncodingUnsupported.Load(model); !ok {
		t.Fatalf("expected unsupported tokenizer model to be cached")
	}
}
