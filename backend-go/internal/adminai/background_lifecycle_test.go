package adminai

import (
	"sync"
	"testing"
)

// TestBackgroundLifecycleConcurrentStop 覆盖 StartBackground/StopBackground 的并发调用：
// 停止信号字段过去无锁读写，-race 下会报数据竞争；重复 Stop 过去也可能 double close。
func TestBackgroundLifecycleConcurrentStop(t *testing.T) {
	s := newTestService(t)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.StartBackground()
			s.StopBackground()
		}()
	}
	wg.Wait()

	// 收敛后再调一次，确认幂等且不会 panic（close 已关闭的 channel）。
	s.StopBackground()
}

// TestBackgroundStopIsIdempotent 覆盖「未启动就 Stop」与「Stop 后重复 Stop」。
func TestBackgroundStopIsIdempotent(t *testing.T) {
	s := newTestService(t)
	s.StopBackground()
	s.StopBackground()

	s.StartBackground()
	s.StopBackground()
	s.StopBackground()
}

// TestBackgroundDoesNotRestartAfterStop 覆盖 Stop 之后 StartBackground 不再拉起后台 goroutine。
func TestBackgroundDoesNotRestartAfterStop(t *testing.T) {
	s := newTestService(t)
	s.StartBackground()
	s.StopBackground()

	s.StartBackground()
	s.bgMu.Lock()
	stopped := s.bgStopped
	cleaner := s.stopCleaner
	capture := s.stopCapture
	s.bgMu.Unlock()
	if !stopped {
		t.Fatal("bgStopped should stay true after StopBackground")
	}
	if cleaner != nil || capture != nil {
		t.Fatalf("stop channels should not be re-created after Stop: cleaner=%v capture=%v", cleaner != nil, capture != nil)
	}
	s.StopBackground()
}
