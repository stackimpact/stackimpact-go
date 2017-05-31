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

	segmentNodes := agent.segmentReporter.segmentNodes
	agent.segmentReporter.report()

	seg1Counter := segmentNodes["seg1"]
	if seg1Counter.name != "seg1" || seg1Counter.measurement < 10 {
		t.Errorf("Measurement of seg1 is too low: %v", seg1Counter.measurement)
	}
}

func TestReadLastDurations(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	agent.segmentReporter.recordSegment("seg1", 10.1)

	if LoadFloat64(agent.segmentReporter.segmentDurations["seg1"]) != 10.1 {
		t.Errorf("Duration of seg1 should be 10.1 but is %v", LoadFloat64(agent.segmentReporter.segmentDurations["seg1"]))
	}

	durations := agent.segmentReporter.readLastDurations()
	if durations["seg1"] != 10.1 {
		t.Errorf("Duration of seg1 should be 10.1 but is %v", durations["seg1"])
	}

	if LoadFloat64(agent.segmentReporter.segmentDurations["seg1"]) != -1 {
		t.Errorf("Duration of seg1 should be -1 but is %v", LoadFloat64(agent.segmentReporter.segmentDurations["seg1"]))
	}

	agent.segmentReporter.recordSegment("seg1", 10.2)
	agent.segmentReporter.recordSegment("seg1", 10.3)

	durations = agent.segmentReporter.readLastDurations()
	if durations["seg1"] != 10.3 {
		t.Errorf("Duration of seg1 should be 10.3 but is %v", durations["seg1"])
	}
}
