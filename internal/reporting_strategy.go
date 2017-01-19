package internal

import (
	"math"
	"sync/atomic"
	"time"
)

type metricFuncType func() float64
type reportFuncType func(trigger string)

type ReportingStrategy struct {
	agent          *Agent
	delay          int
	interval       int
	metricFunc     metricFuncType
	reportFunc     reportFuncType
	anomalyCounter int32
	measurements   []float64
}

func newReportingStrategy(agent *Agent, delay int, interval int, metricFunc metricFuncType, reportFunc reportFuncType) *ReportingStrategy {
	rs := &ReportingStrategy{
		agent:          agent,
		delay:          delay,
		interval:       interval,
		metricFunc:     metricFunc,
		reportFunc:     reportFunc,
		anomalyCounter: 0,
		measurements:   make([]float64, 0),
	}

	return rs
}

func (rs *ReportingStrategy) readAnomalyCounter() int32 {
	return atomic.LoadInt32(&rs.anomalyCounter)
}

func (rs *ReportingStrategy) incAnomalyCounter() int32 {
	return atomic.AddInt32(&rs.anomalyCounter, 1)
}

func (rs *ReportingStrategy) resetAnomalyCounter() {
	atomic.StoreInt32(&rs.anomalyCounter, 0)
}

func (rs *ReportingStrategy) start() {

	if rs.metricFunc != nil {
		anomalyTicker := time.NewTicker(1 * time.Second)
		go func() {
			defer rs.agent.recoverAndLog()

			for {
				select {
				case <-anomalyTicker.C:
					if rs.checkAnomaly() && rs.readAnomalyCounter() < 1 {
						rs.incAnomalyCounter()
						rs.executeReport(TriggerAnomaly)
					}
				}
			}
		}()
	}

	delayTimer := time.NewTimer(time.Duration(rs.delay) * time.Second)
	go func() {
		defer rs.agent.recoverAndLog()

		<-delayTimer.C
		rs.executeReport(TriggerTimer)
		rs.resetAnomalyCounter()

		intervalTicker := time.NewTicker(time.Duration(rs.interval) * time.Second)
		go func() {
			defer rs.agent.recoverAndLog()

			for {
				select {
				case <-intervalTicker.C:
					rs.executeReport(TriggerTimer)
					rs.resetAnomalyCounter()
				}
			}
		}()
	}()
}

func (rs *ReportingStrategy) checkAnomaly() bool {
	if rs.agent.isProfilingActive() {
		return false
	}

	m := rs.metricFunc()
	rs.measurements = append(rs.measurements, m)
	l := len(rs.measurements)

	if l < 60 {
		return false
	} else if l > 60 {
		rs.measurements = rs.measurements[l-60 : l]
	}

	mean, dev := stdev(rs.measurements[0:30])

	recent := rs.measurements[50:60]
	anomalies := 0
	for _, m := range recent {
		if m > mean+2*dev && m > mean+mean*0.5 {
			anomalies++
		}
	}

	return anomalies >= 5
}

func stdev(numbers []float64) (float64, float64) {
	mean := 0.0
	for _, number := range numbers {
		mean += number
	}
	mean = mean / float64(len(numbers))

	total := 0.0
	for _, number := range numbers {
		total += math.Pow(number-mean, 2)
	}

	variance := total / float64(len(numbers)-1)

	return mean, math.Sqrt(variance)
}

func (rs *ReportingStrategy) executeReport(trigger string) {
	rs.agent.profilingLock.Lock()
	rs.agent.profilingActive = true

	defer func() {
		rs.agent.profilingActive = false
		rs.agent.profilingLock.Unlock()
	}()

	rs.reportFunc(trigger)
}
