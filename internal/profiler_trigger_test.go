package internal

import (
	"math/rand"
	"testing"
	"time"
)

func TestUpdateLastReportTs(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	pt := newProfilerTrigger(agent, 100, 100, nil, nil)

	if pt.lastTriggerTS != 0 {
		t.Errorf("Last report timestamp should be 0, but is %v", pt.lastTriggerTS)
	}

	now := time.Now().Unix()
	pt.updateLastTriggerTS()

	if pt.lastTriggerTS < now {
		t.Errorf("Last report timestamp should be >= %v, but is %v", now, pt.lastTriggerTS)
	}
}

func TestDetectAnomaly(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	pt := newProfilerTrigger(agent, 100, 100,
		func() map[string]float64 {
			return map[string]float64{"test-val": 90.0}
		},
		nil)

	pt.metrics["test-val"] = make([]float64, 0, 45)
	for i := 0; i < 30; i++ {
		pt.metrics["test-val"] = append(pt.metrics["test-val"], float64(rand.Intn(20)))
	}
	for i := 0; i < 15; i++ {
		pt.metrics["test-val"] = append(pt.metrics["test-val"], float64(90.0))
	}

	if !pt.detectAnomaly() {
		t.Error("Didn't detect anomaly")
	}
}

func TestDetectAnomalyNotEnoughData(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	pt := newProfilerTrigger(agent, 100, 100,
		func() map[string]float64 {
			return map[string]float64{"test-val": 90.0}
		},
		nil)

	pt.metrics["test-val"] = make([]float64, 0, 45)
	for i := 0; i < 15; i++ {
		pt.metrics["test-val"] = append(pt.metrics["test-val"], float64(rand.Intn(20)))
	}
	for i := 0; i < 15; i++ {
		pt.metrics["test-val"] = append(pt.metrics["test-val"], float64(90.0))
	}

	if pt.detectAnomaly() {
		t.Error("Did detect anomaly")
	}
}

func TestDetectAnomalyFalse(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	pt := newProfilerTrigger(agent, 100, 100,
		func() map[string]float64 {
			return map[string]float64{"test-val": 10.0}
		},
		nil)

	pt.metrics["test-val"] = make([]float64, 0, 45)
	for i := 0; i < 45; i++ {
		pt.metrics["test-val"] = append(pt.metrics["test-val"], float64(rand.Intn(20)))
	}

	if pt.detectAnomaly() {
		t.Error("Did detect anomaly")
	}
}

func TestAnomalyReport(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	reportCount := 0

	pt := newProfilerTrigger(agent, 3, 100,
		func() map[string]float64 {
			return map[string]float64{"test-val": 90.0}
		},
		func(trigger string) {
			if trigger == "anomaly" {
				reportCount++
			}
		},
	)

	pt.metrics["test-val"] = make([]float64, 0, 45)
	for i := 0; i < 30; i++ {
		pt.metrics["test-val"] = append(pt.metrics["test-val"], float64(rand.Intn(20)))
	}
	for i := 0; i < 15; i++ {
		pt.metrics["test-val"] = append(pt.metrics["test-val"], float64(90.0))
	}

	pt.start()

	testTimer := time.NewTimer(2 * time.Second)
	done := make(chan bool)
	go func() {
		<-testTimer.C

		if reportCount < 1 {
			t.Error("report func was not called")
		}

		if pt.lastTriggerTS == 0 {
			t.Error("reportCounter is 0 after reporting")
		}

		done <- true
	}()
	<-done
}

func TestTimerReport(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	reportCount := 0
	pt := newProfilerTrigger(agent, 1, 1,
		func() map[string]float64 {
			return map[string]float64{"test-val": 20.0}
		},
		func(trigger string) {
			if trigger == "timer" {
				reportCount++
			}
		},
	)
	pt.start()

	testTimer := time.NewTimer(3 * time.Second)
	done := make(chan bool)
	go func() {
		<-testTimer.C

		if reportCount < 2 {
			t.Error("report func was called less than 2 times")
		}

		done <- true
	}()
	<-done
}
