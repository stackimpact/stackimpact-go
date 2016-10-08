package stackimpact

import (
	"math/rand"
	"testing"
	"time"
)

func TestCheckAnomaly(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	rs := newReportingStrategy(agent, 100, 100, func() float64 { return 90.0 }, nil)
	for i := 0; i < 50; i++ {
		rs.measurements = append(rs.measurements, float64(rand.Intn(20)))
	}
	for i := 50; i < 55; i++ {
		rs.measurements = append(rs.measurements, float64(rand.Intn(20)))
	}
	for i := 55; i < 60; i++ {
		rs.measurements = append(rs.measurements, float64(20+rand.Intn(80)))
	}

	if !rs.checkAnomaly() {
		t.Error("Didn't detect anomaly")
	}
}

func TestCheckAnomalyFalse(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	rs := newReportingStrategy(agent, 100, 100, func() float64 { return 10.0 }, nil)
	for i := 0; i < 60; i++ {
		rs.measurements = append(rs.measurements, float64(rand.Intn(20)))
	}

	if rs.checkAnomaly() {
		t.Error("Did detect anomaly")
	}
}

func TestAnomalyReport(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	reportCount := 0
	rs := newReportingStrategy(agent, 3, 100,
		func() float64 {
			return 90.0
		},
		func(trigger string) {
			if trigger == "anomaly" {
				reportCount++
			}
		},
	)
	for i := 0; i < 50; i++ {
		rs.measurements = append(rs.measurements, float64(rand.Intn(20)))
	}
	for i := 50; i < 55; i++ {
		rs.measurements = append(rs.measurements, float64(rand.Intn(20)))
	}
	for i := 55; i < 60; i++ {
		rs.measurements = append(rs.measurements, float64(20+rand.Intn(80)))
	}
	rs.start()

	testTimer := time.NewTimer(2 * time.Second)
	done := make(chan bool)
	go func() {
		<-testTimer.C

		if reportCount < 1 {
			t.Error("report func was not called")
		}

		if !rs.anomalyReported {
			t.Error("anomalyReported is false after reporting")
		}

		done <- true
	}()
	<-done
}

func TestTimerReport(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	reportCount := 0
	rs := newReportingStrategy(agent, 1, 1,
		func() float64 {
			return 20.0
		},
		func(trigger string) {
			if trigger == "timer" {
				reportCount++
			}
		},
	)
	rs.start()

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
