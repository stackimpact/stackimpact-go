package stackimpact

import (
	"testing"
	"time"
)

func TestRecordSegment(t *testing.T) {
	agent := NewAgent()

	done1 := make(chan bool)

	var seg1 *Segment
	var sub1 *Segment
	go func() {
		seg1 = agent.MeasureSegment("seg1")
		defer seg1.Stop()

		time.Sleep(30 * time.Millisecond)

		done2 := make(chan bool)

		go func() {
			sub1 = agent.MeasureSubsegment("seg1", "sub1")
			defer sub1.Stop()

			time.Sleep(70 * time.Millisecond)

			done2 <- true
		}()

		<-done2

		done1 <- true
	}()

	<-done1

	time.Sleep(10 * time.Millisecond)

	if seg1.Duration < 50 {
		t.Errorf("Duration of seg1 is too low: %v", seg1.Duration)
	}

	if sub1.Duration < 35 {
		t.Errorf("Duration of sub1 is too low: %v", sub1.Duration)
	}
}

func TestRecoverPanic(t *testing.T) {
	agent := NewAgent()

	done := make(chan bool)

	go func() {
		defer func() {
			if err := recover(); err != nil {
				t.Error("panic1 unrecovered")
			}
		}()
		defer agent.RecordAndRecoverPanic()
		defer func() {
			done <- true
		}()

		panic("panic1")
	}()

	<-done
}
