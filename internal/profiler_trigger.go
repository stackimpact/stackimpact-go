package internal

import (
	"math"
	"sync/atomic"
	"time"
)

type metricFuncType func() map[string]float64
type reportFuncType func(trigger string)

type ProfilerTrigger struct {
	agent         *Agent
	delay         int
	interval      int
	metricFunc    metricFuncType
	reportFunc    reportFuncType
	lastTriggerTS int64
	metrics       map[string][]float64
}

func newProfilerTrigger(agent *Agent, delay int, interval int, metricFunc metricFuncType, reportFunc reportFuncType) *ProfilerTrigger {
	pt := &ProfilerTrigger{
		agent:         agent,
		delay:         delay,
		interval:      interval,
		metricFunc:    metricFunc,
		reportFunc:    reportFunc,
		lastTriggerTS: 0,
		metrics:       make(map[string][]float64),
	}

	return pt
}

func (pt *ProfilerTrigger) updateLastTriggerTS() bool {
	now := time.Now().Unix()
	ts := atomic.LoadInt64(&pt.lastTriggerTS)

	if ts < now-int64(pt.interval/3) {
		return atomic.CompareAndSwapInt64(&pt.lastTriggerTS, ts, now)
	}

	return false
}

func (pt *ProfilerTrigger) start() {
	if pt.metricFunc != nil {
		anomalyTicker := time.NewTicker(1 * time.Second)
		go func() {
			defer pt.agent.recoverAndLog()

			for {
				select {
				case <-anomalyTicker.C:
					if pt.detectAnomaly() {
						go pt.executeReport(TriggerAnomaly)
					}
				}
			}
		}()
	}

	delayTimer := time.NewTimer(time.Duration(pt.delay) * time.Second)
	go func() {
		defer pt.agent.recoverAndLog()

		<-delayTimer.C
		go pt.executeReport(TriggerTimer)

		intervalTicker := time.NewTicker(time.Duration(pt.interval) * time.Second)
		go func() {
			defer pt.agent.recoverAndLog()

			for {
				select {
				case <-intervalTicker.C:
					go pt.executeReport(TriggerTimer)
				}
			}
		}()
	}()
}

func (pt *ProfilerTrigger) detectAnomaly() bool {
	if pt.agent.isProfilerActive() {
		return false
	}

	lastValues := pt.metricFunc()

	// Update metrics map with last values
	for name, lastValue := range lastValues {
		values, exists := pt.metrics[name]
		if !exists {
			values = make([]float64, 0, 45)
		}

		values = append(values, lastValue)

		l := len(values)
		if l > 45 {
			values = values[l-45 : l]
		}

		pt.metrics[name] = values
	}

	// Access metrics map for detection
	for name, lastValue := range lastValues {
		values := pt.metrics[name]

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
			pt.agent.log("Anomaly detected in %v metric: %v, values=%v", name, lastValue, values)
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

func (pt *ProfilerTrigger) executeReport(trigger string) {
	defer pt.agent.recoverAndLog()

	if !pt.updateLastTriggerTS() {
		return
	}

	if !pt.agent.setProfilerActive() {
		return
	}

	defer func() {
		pt.agent.setProfilerInactive()
	}()

	pt.reportFunc(trigger)
}
