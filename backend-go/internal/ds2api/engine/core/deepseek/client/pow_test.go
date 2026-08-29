package client

import (
	"context"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	powpkg "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/pow"
)

func TestPreloadPowNoOp(t *testing.T) {
	client := NewClient(nil, nil)
	if err := client.PreloadPow(context.Background()); err != nil {
		t.Fatalf("PreloadPow should be no-op, got error: %v", err)
	}
}

func TestComputePowUnsupportedAlgorithm(t *testing.T) {
	_, err := ComputePow(context.Background(), map[string]any{"algorithm": "unknown"})
	if err == nil {
		t.Fatal("expected error for unsupported algorithm")
	}
}

func TestGetPowForTargetIgnoresPrefetchedChallengeCache(t *testing.T) {
	targetPath := dsprotocol.DeepSeekCompletionTargetPath
	cachedChallenge := testPowChallenge(7, targetPath, "cached")
	freshChallenge := testPowChallenge(42, targetPath, "fresh")

	body, err := json.Marshal(map[string]any{
		"code": 0,
		"msg":  "ok",
		"data": map[string]any{
			"biz_code": 0,
			"biz_data": map[string]any{
				"challenge": freshChallenge,
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal pow response: %v", err)
	}

	callCount := 0
	client := &Client{
		regular: doerFunc(func(req *http.Request) (*http.Response, error) {
			callCount++
			reqBody, _ := io.ReadAll(req.Body)
			if !strings.Contains(string(reqBody), `"target_path":"`+targetPath+`"`) {
				t.Fatalf("expected completion target_path in pow request, got %s", string(reqBody))
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body))), Request: req}, nil
		}),
		fallback:   &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) { return nil, nil })},
		maxRetries: 1,
		powCache:   newPowChallengeCache(),
	}
	client.powCache.set("acct", targetPath, cachedChallenge)

	header, err := client.GetPowForTarget(context.Background(), &auth.RequestAuth{
		DeepSeekToken: "token",
		AccountID:     "acct",
		TriedAccounts: map[string]bool{},
	}, targetPath, 1)
	if err != nil {
		t.Fatalf("GetPowForTarget error: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected fresh upstream PoW request, got %d calls", callCount)
	}

	decoded, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		t.Fatalf("decode pow header: %v", err)
	}
	var powHeader map[string]any
	if err := json.Unmarshal(decoded, &powHeader); err != nil {
		t.Fatalf("unmarshal pow header: %v", err)
	}
	if powHeader["signature"] != "fresh" {
		t.Fatalf("expected fresh challenge signature, got %#v", powHeader["signature"])
	}
}

func testPowChallenge(answer int64, targetPath string, signature string) map[string]any {
	expireAt := time.Now().Add(time.Hour).Unix()
	salt := "salt-" + signature
	hash := powpkg.DeepSeekHashV1([]byte(powpkg.BuildPrefix(salt, expireAt) + strconv.FormatInt(answer, 10)))
	return map[string]any{
		"algorithm":   "DeepSeekHashV1",
		"challenge":   fmtHex(hash[:]),
		"salt":        salt,
		"expire_at":   expireAt,
		"difficulty":  int64(1000),
		"signature":   signature,
		"target_path": targetPath,
	}
}

func fmtHex(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&0x0f]
	}
	return string(out)
}
