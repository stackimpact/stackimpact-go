package internal

import (
	"testing"
	"time"
)

func TestRecordSpan(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	agent.spanReporter.reset()
	agent.spanReporter.started.Set()

	for i := 0; i < 100; i++ {
		go func() {
			defer agent.spanReporter.recordSpan("span1", 10)

			time.Sleep(10 * time.Millisecond)
		}()
	}

	time.Sleep(150 * time.Millisecond)

	spanNodes := agent.spanReporter.spanNodes
	agent.spanReporter.report()

	span1Counter := spanNodes["span1"]
	if span1Counter.name != "span1" || span1Counter.measurement < 10 {
		t.Errorf("Measurement of span1 is too low: %v", span1Counter.measurement)
	}
}
