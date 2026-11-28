package serveragent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestSFTPUploadStreamsAgentFileInChunks 锁住面板 SFTP 上传的流式分块行为:
// 整文件不得一次读入内存,必须按 agentFileChunkSize 分片下发;首块 truncate、
// 后续块按 offset 续写,响应回报真实路径与总大小。
func TestSFTPUploadStreamsAgentFileInChunks(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts (id, name, host, username, auth_type) VALUES ('stream-agent', 'stream', '', 'root', 'password')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}

	type writeCall struct {
		size     int
		offset   int64
		truncate bool
		realPath bool
	}
	var (
		mu    sync.Mutex
		calls []writeCall
	)
	service.registry.Register("stream-agent", &taskReplySocket{
		t:       t,
		service: service,
		reply: func(taskType int, data string) string {
			if taskType != agentFileWriteBase64Task {
				t.Fatalf("unexpected task type: %d", taskType)
			}
			var payload struct {
				Data     string `json:"data"`
				Offset   int64  `json:"offset"`
				Truncate bool   `json:"truncate"`
				RealPath bool   `json:"realPath"`
			}
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				t.Fatalf("decode write task: %v data=%s", err, data)
			}
			decoded, err := base64.StdEncoding.DecodeString(payload.Data)
			if err != nil {
				t.Fatalf("decode chunk base64: %v", err)
			}
			mu.Lock()
			calls = append(calls, writeCall{size: len(decoded), offset: payload.Offset, truncate: payload.Truncate, realPath: payload.RealPath})
			mu.Unlock()
			return `{"message":"文件保存成功","realPath":"/opt/app/big.bin"}`
		},
	}).SetMetadata("agent_version", minAgentFileRealPathVersion)

	// 2.5 个分块:若实现退化成「整文件一次写入」,这里只会收到 1 次任务。
	totalSize := agentFileChunkSize*2 + agentFileChunkSize/2
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("serverId", "stream-agent"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("path", "/opt/app"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("relativePath", "big.bin"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "big.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("A"), totalSize)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/server/sftp/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodePayload(t, rec)
	if got := payload["size"]; got != float64(totalSize) {
		t.Fatalf("reported size=%v want %d", got, totalSize)
	}
	if got := payload["realPath"]; got != "/opt/app/big.bin" {
		t.Fatalf("reported realPath=%v", got)
	}

	mu.Lock()
	defer mu.Unlock()
	wantChunks := 3
	if len(calls) != wantChunks {
		t.Fatalf("expected %d chunk writes, got %d (%#v)", wantChunks, len(calls), calls)
	}
	var sum int
	for i, call := range calls {
		if call.size > agentFileChunkSize {
			t.Fatalf("chunk %d size %d exceeds agentFileChunkSize %d", i, call.size, agentFileChunkSize)
		}
		if call.realPath != true {
			t.Fatalf("chunk %d missing realPath=true", i)
		}
		wantTruncate := i == 0
		if call.truncate != wantTruncate {
			t.Fatalf("chunk %d truncate=%v want %v", i, call.truncate, wantTruncate)
		}
		if call.offset != int64(sum) {
			t.Fatalf("chunk %d offset=%d want %d", i, call.offset, sum)
		}
		sum += call.size
	}
	if sum != totalSize {
		t.Fatalf("total streamed bytes=%d want %d", sum, totalSize)
	}
}
