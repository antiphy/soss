package logic

import (
	"sync/atomic"
	"testing"
)

// TestStopNoNegativeWaitGroup 守护优雅退出路径：
// goAsync 是唯一负责 wg.Add/Done 的地方，任何后台任务都不得自行调用 m.wg.Done()，
// 否则一次 Add 配对两次 Done 会让 WaitGroup 计数器变负并 panic。
func TestStopNoNegativeWaitGroup(t *testing.T) {
	for i := 0; i < 50; i++ {
		m := &Manager{stopCh: make(chan struct{})}
		var ran atomic.Int64

		// 长跑任务：收到 stopCh 后返回，模拟 capacityWorker。
		m.goAsync(func() {
			<-m.stopCh
			ran.Add(1)
		})
		// 短任务：模拟 warm-up。
		m.goAsync(func() { ran.Add(1) })

		m.Stop()
		m.Stop()                                                 // Stop 幂等，重复调用不得 panic
		m.goAsync(func() { t.Error("task started after Stop") }) // Stop 后不得再起任务

		if got := ran.Load(); got != 2 {
			t.Fatalf("expected 2 finished tasks, got %d", got)
		}
	}
}
