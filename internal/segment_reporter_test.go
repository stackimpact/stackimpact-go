package internal

import (
	"testing"
	"time"
)

func TestRecordSegment(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	for i := 0; i < 100; i++ {
		go func() {
			defer agent.segmentReporter.recordSegment("seg1", 10)

			time.Sleep(10 * time.Millisecond)
		}()
	}

	time.Sleep(150 * time.Millisecond)

	segmentCounters := agent.segmentReporter.segmentCounters
	agent.segmentReporter.report("timer")

	seg1Counter := segmentCounters["seg1"]
	if seg1Counter.name != "seg1" || seg1Counter.measurement < 10 {
		t.Errorf("Measurement of seg1 is too low: %v", seg1Counter.measurement)
	}
}
