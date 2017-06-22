package internal

import (
	"testing"
	"time"
)

func TestTimerReport(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	recordCount := 0
	reportCount := 0
	pt := newProfilerScheduler(agent, 10, 1, 100,
		func(duration int64) {
			recordCount++
		},
		func() {
			reportCount++
		},
	)
	pt.start()

	testTimer := time.NewTimer(500 * time.Millisecond)
	done := make(chan bool)
	go func() {
		<-testTimer.C

		if recordCount < 5 {
			t.Error("record func was called less than 9 times")
		}

		if reportCount < 1 {
			t.Error("report func was called less than 1 times")
		}

		done <- true
	}()
	<-done
}
