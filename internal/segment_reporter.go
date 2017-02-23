package internal

import (
	"sync"
)

type SegmentReporter struct {
	agent             *Agent
	reportingStrategy *ReportingStrategy
	recordLock        *sync.Mutex
	segmentCounters   map[string]*BreakdownNode
}

func newSegmentReporter(agent *Agent) *SegmentReporter {
	sr := &SegmentReporter{
		agent:             agent,
		reportingStrategy: nil,
		recordLock:        &sync.Mutex{},
		segmentCounters:   make(map[string]*BreakdownNode),
	}

	sr.reportingStrategy = newReportingStrategy(agent, 60, 60, nil,
		func(trigger string) {
			sr.agent.log("Segment report triggered by reporting strategy, trigger=%v", trigger)
			sr.report(trigger)
		},
	)

	return sr
}

func (sr *SegmentReporter) start() {
	sr.reportingStrategy.start()
}

func (sr *SegmentReporter) recordSegment(name string, duration int64) {
	go sr.recordSegmentSync(name, duration)
}

func (sr *SegmentReporter) recordSegmentSync(name string, duration int64) {
	if name == "" {
		sr.agent.log("Empty segment name")
		return
	}

	sr.recordLock.Lock()

	segmentCounter, exists := sr.segmentCounters[name]
	if !exists {
		segmentCounter = newBreakdownNode(name)
		sr.segmentCounters[name] = segmentCounter
	}

	segmentCounter.updateP95(float64(duration))

	sr.recordLock.Unlock()
}

func (sr *SegmentReporter) report(trigger string) {
	sr.recordLock.Lock()

	for _, segmentCounter := range sr.segmentCounters {
		segmentCounter.evaluateP95()

		metric := newMetric(sr.agent, TypeTrace, CategorySegmentTrace, segmentCounter.name, UnitMillisecond)
		metric.createMeasurement(trigger, segmentCounter.measurement, segmentCounter)
		sr.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	sr.segmentCounters = make(map[string]*BreakdownNode)

	sr.recordLock.Unlock()
}
