package internal

import (
	"math/rand"
	"testing"
	"time"
)

func TestUpdateLastReportTs(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	rt := newReportTrigger(agent, 100, 100, nil, nil)

	if rt.lastReportTs != 0 {
		t.Errorf("Last report timestamp should be 0, but is %v", rt.lastReportTs)
	}

	now := time.Now().Unix()
	rt.updateLastReportTs()

	if rt.lastReportTs < now {
		t.Errorf("Last report timestamp should be >= %v, but is %v", now, rt.lastReportTs)
	}
}

func TestDetectAnomaly(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	rt := newReportTrigger(agent, 100, 100,
		func() map[string]float64 {
			return map[string]float64{"test-val": 90.0}
		},
		nil)

	rt.metrics["test-val"] = make([]float64, 0, 45)
	for i := 0; i < 30; i++ {
		rt.metrics["test-val"] = append(rt.metrics["test-val"], float64(rand.Intn(20)))
	}
	for i := 0; i < 15; i++ {
		rt.metrics["test-val"] = append(rt.metrics["test-val"], float64(90.0))
	}

	if !rt.detectAnomaly() {
		t.Error("Didn't detect anomaly")
	}
}

func TestDetectAnomalyNotEnoughData(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	rt := newReportTrigger(agent, 100, 100,
		func() map[string]float64 {
			return map[string]float64{"test-val": 90.0}
		},
		nil)

	rt.metrics["test-val"] = make([]float64, 0, 45)
	for i := 0; i < 15; i++ {
		rt.metrics["test-val"] = append(rt.metrics["test-val"], float64(rand.Intn(20)))
	}
	for i := 0; i < 15; i++ {
		rt.metrics["test-val"] = append(rt.metrics["test-val"], float64(90.0))
	}

	if rt.detectAnomaly() {
		t.Error("Did detect anomaly")
	}
}

func TestDetectAnomalyFalse(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	rt := newReportTrigger(agent, 100, 100,
		func() map[string]float64 {
			return map[string]float64{"test-val": 10.0}
		},
		nil)

	rt.metrics["test-val"] = make([]float64, 0, 45)
	for i := 0; i < 45; i++ {
		rt.metrics["test-val"] = append(rt.metrics["test-val"], float64(rand.Intn(20)))
	}

	if rt.detectAnomaly() {
		t.Error("Did detect anomaly")
	}
}

func TestAnomalyReport(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	reportCount := 0

	rt := newReportTrigger(agent, 3, 100,
		func() map[string]float64 {
			return map[string]float64{"test-val": 90.0}
		},
		func(trigger string) {
			if trigger == "anomaly" {
				reportCount++
			}
		},
	)

	rt.metrics["test-val"] = make([]float64, 0, 45)
	for i := 0; i < 30; i++ {
		rt.metrics["test-val"] = append(rt.metrics["test-val"], float64(rand.Intn(20)))
	}
	for i := 0; i < 15; i++ {
		rt.metrics["test-val"] = append(rt.metrics["test-val"], float64(90.0))
	}

	rt.start()

	testTimer := time.NewTimer(2 * time.Second)
	done := make(chan bool)
	go func() {
		<-testTimer.C

		if reportCount < 1 {
			t.Error("report func was not called")
		}

		if rt.lastReportTs == 0 {
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
	rt := newReportTrigger(agent, 1, 1,
		func() map[string]float64 {
			return map[string]float64{"test-val": 20.0}
		},
		func(trigger string) {
			if trigger == "timer" {
				reportCount++
			}
		},
	)
	rt.start()

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
