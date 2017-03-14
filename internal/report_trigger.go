package internal

import (
	"math"
	"sync/atomic"
	"time"
)

type metricFuncType func() map[string]float64
type reportFuncType func(trigger string)

type ReportTrigger struct {
	agent        *Agent
	delay        int
	interval     int
	metricFunc   metricFuncType
	reportFunc   reportFuncType
	lastReportTs int64
	metrics      map[string][]float64
}

func newReportTrigger(agent *Agent, delay int, interval int, metricFunc metricFuncType, reportFunc reportFuncType) *ReportTrigger {
	rt := &ReportTrigger{
		agent:        agent,
		delay:        delay,
		interval:     interval,
		metricFunc:   metricFunc,
		reportFunc:   reportFunc,
		lastReportTs: 0,
		metrics:      make(map[string][]float64),
	}

	return rt
}

func (rt *ReportTrigger) updateLastReportTs() bool {
	now := time.Now().Unix()
	ts := atomic.LoadInt64(&rt.lastReportTs)

	if ts < now-int64(rt.interval/3) {
		return atomic.CompareAndSwapInt64(&rt.lastReportTs, ts, now)
	}

	return false
}

func (rt *ReportTrigger) start() {
	if rt.metricFunc != nil {
		anomalyTicker := time.NewTicker(1 * time.Second)
		go func() {
			defer rt.agent.recoverAndLog()

			for {
				select {
				case <-anomalyTicker.C:
					if !rt.agent.isProfilingActive() && rt.detectAnomaly() && rt.updateLastReportTs() {
						go rt.executeReport(TriggerAnomaly)
					}
				}
			}
		}()
	}

	delayTimer := time.NewTimer(time.Duration(rt.delay) * time.Second)
	go func() {
		defer rt.agent.recoverAndLog()

		<-delayTimer.C
		if rt.updateLastReportTs() {
			go rt.executeReport(TriggerTimer)
		}

		intervalTicker := time.NewTicker(time.Duration(rt.interval) * time.Second)
		go func() {
			defer rt.agent.recoverAndLog()

			for {
				select {
				case <-intervalTicker.C:
					if rt.updateLastReportTs() {
						go rt.executeReport(TriggerTimer)
					}
				}
			}
		}()
	}()
}

func (rt *ReportTrigger) detectAnomaly() bool {
	lastValues := rt.metricFunc()

	// Update metrics map with last values
	for name, lastValue := range lastValues {
		values, exists := rt.metrics[name]
		if !exists {
			values = make([]float64, 0, 45)
		}

		values = append(values, lastValue)

		l := len(values)
		if l > 45 {
			values = values[l-45 : l]
		}

		rt.metrics[name] = values
	}

	// Access metrics map for detection
	for name, lastValue := range lastValues {
		values := rt.metrics[name]

		if len(values) < 45 {
			continue
		}

		mean, dev := stdev(values[0:30])
		recent := values[30:45]
		anomalies := 0

		for _, v := range recent {
			if v > mean+2*dev && v > mean+mean*0.5 {
				anomalies++
			}
		}

		if anomalies >= 5 {
			rt.agent.log("Anomaly detected in %v metric: %v, values=%v", name, lastValue, values)
			return true
		}
	}

	return false
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

func (rt *ReportTrigger) executeReport(trigger string) {
	defer rt.agent.recoverAndLog()

	rt.agent.profilingLock.Lock()
	rt.agent.profilingActive = true

	defer func() {
		rt.agent.profilingActive = false
		rt.agent.profilingLock.Unlock()
	}()

	rt.reportFunc(trigger)
}
