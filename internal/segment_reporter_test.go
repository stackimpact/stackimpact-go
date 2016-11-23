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
			defer agent.RecordSegment([]string{"seg1"}, 10)

			time.Sleep(10 * time.Millisecond)

			done := make(chan bool)

			go func() {
				defer agent.RecordSegment([]string{"seg1", "seg2"}, 7)

				time.Sleep(7 * time.Millisecond)

				done <- true
			}()

			<-done
		}()
	}

	time.Sleep(50 * time.Millisecond)

	segmentGraphs := agent.segmentReporter.segmentGraphs
	agent.segmentReporter.report("timer")

	seg1Graph := segmentGraphs["seg1"]
	if seg1Graph.name != "seg1" || seg1Graph.measurement < 10 {
		t.Errorf("Measurement of seg1 is too low: %v", seg1Graph.measurement)
	}

	seg2Graph := segmentGraphs["seg1"].children["seg2"]
	if seg2Graph.name != "seg2" || seg2Graph.measurement < 7 {
		t.Errorf("Measurement of seg2 is too low: %v", seg2Graph.measurement)
	}
}
