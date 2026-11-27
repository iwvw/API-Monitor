package serveragent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 并发分块管道:顺序读、乱序并发写,最终文件必须与源逐字节一致,sha256 也必须匹配。
func TestCopyChunksConcurrentlyPreservesBytes(t *testing.T) {
	const chunkSize = 1024
	content := make([]byte, chunkSize*5+137) // 5 整块 + 1 残块,覆盖末块短读
	for i := range content {
		content[i] = byte((i*31 + 7) & 0xff)
	}

	var mu sync.Mutex
	written := map[int64][]byte{}
	write := func(_ context.Context, data []byte, offset int64) error {
		buf := make([]byte, len(data))
		copy(buf, data)
		mu.Lock()
		written[offset] = buf
		mu.Unlock()
		return nil
	}

	n, sum, err := copyChunksConcurrently(context.Background(), bytes.NewReader(content), chunkSize, 4, write)
	if err != nil {
		t.Fatalf("copyChunksConcurrently err: %v", err)
	}
	if n != int64(len(content)) {
		t.Fatalf("写入字节数=%d, want %d", n, len(content))
	}
	want := sha256.Sum256(content)
	if sum != hex.EncodeToString(want[:]) {
		t.Fatalf("sha256 不匹配: got %s", sum)
	}

	// 按 offset 重组文件,必须与源逐字节一致(切分与偏移都正确,无空洞/重叠)
	rebuilt := make([]byte, len(content))
	covered := 0
	for off, data := range written {
		if off < 0 || off+int64(len(data)) > int64(len(content)) {
			t.Fatalf("写入越界: offset=%d len=%d", off, len(data))
		}
		copy(rebuilt[off:], data)
		covered += len(data)
	}
	if covered != len(content) {
		t.Fatalf("覆盖字节=%d, want %d(有空洞或重叠)", covered, len(content))
	}
	if !bytes.Equal(rebuilt, content) {
		t.Fatal("重组后的文件与源不一致")
	}
}

// 空文件:不触发任何写入,返回 0 字节与空内容摘要。
func TestCopyChunksConcurrentlyEmpty(t *testing.T) {
	n, sum, err := copyChunksConcurrently(context.Background(), bytes.NewReader(nil), 1024, 4,
		func(context.Context, []byte, int64) error {
			t.Fatal("空文件不应触发写入")
			return nil
		})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 0 {
		t.Fatalf("字节数=%d, want 0", n)
	}
	empty := sha256.Sum256(nil)
	if sum != hex.EncodeToString(empty[:]) {
		t.Fatalf("空内容 sha256 不符: %s", sum)
	}
}

// 任一块写入失败必须向上返回错误且不卡死(不能因为 worker 提前退出而阻塞读循环)。
func TestCopyChunksConcurrentlyPropagatesWriteError(t *testing.T) {
	const chunkSize = 1024
	content := make([]byte, chunkSize*8)
	write := func(_ context.Context, data []byte, offset int64) error {
		if offset == chunkSize*3 {
			return fmt.Errorf("boom at %d", offset)
		}
		return nil
	}
	if _, _, err := copyChunksConcurrently(context.Background(), bytes.NewReader(content), chunkSize, 4, write); err == nil {
		t.Fatal("写入失败必须返回错误")
	}
}

// 关键行为:写回调必须真正并发执行(否则「并发流水线」名不副实,等价于串行)。
// 用带延迟的 write 观察在途峰值,concurrency=4 时应 >1。
func TestCopyChunksConcurrentlyOverlapsWrites(t *testing.T) {
	const chunkSize = 1024
	content := make([]byte, chunkSize*8)

	var inFlight, maxInFlight int64
	write := func(_ context.Context, _ []byte, _ int64) error {
		cur := atomic.AddInt64(&inFlight, 1)
		for {
			old := atomic.LoadInt64(&maxInFlight)
			if cur <= old || atomic.CompareAndSwapInt64(&maxInFlight, old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt64(&inFlight, -1)
		return nil
	}

	if _, _, err := copyChunksConcurrently(context.Background(), bytes.NewReader(content), chunkSize, 4, write); err != nil {
		t.Fatalf("err: %v", err)
	}
	if atomic.LoadInt64(&maxInFlight) < 2 {
		t.Fatalf("写回调没有真正并发,最大在途=%d", maxInFlight)
	}
}

func TestAgentFileCopyConcurrencyValue(t *testing.T) {
	t.Setenv("API_MONITOR_FILE_COPY_CONCURRENCY", "")
	if got := agentFileCopyConcurrencyValue(); got != agentFileCopyConcurrency {
		t.Fatalf("默认并发=%d, want %d", got, agentFileCopyConcurrency)
	}
	t.Setenv("API_MONITOR_FILE_COPY_CONCURRENCY", "1")
	if got := agentFileCopyConcurrencyValue(); got != 1 {
		t.Fatalf("env=1 -> %d", got)
	}
	t.Setenv("API_MONITOR_FILE_COPY_CONCURRENCY", "8")
	if got := agentFileCopyConcurrencyValue(); got != 8 {
		t.Fatalf("env=8 -> %d", got)
	}
	t.Setenv("API_MONITOR_FILE_COPY_CONCURRENCY", "99") // 越界回退默认
	if got := agentFileCopyConcurrencyValue(); got != agentFileCopyConcurrency {
		t.Fatalf("越界应回退默认, got %d", got)
	}
	t.Setenv("API_MONITOR_FILE_COPY_CONCURRENCY", "abc")
	if got := agentFileCopyConcurrencyValue(); got != agentFileCopyConcurrency {
		t.Fatalf("非法应回退默认, got %d", got)
	}
}
