package internal

import (
	"sync"
)

type SegmentReporter struct {
	agent             *Agent
	reportingStrategy *ReportingStrategy
	recordLock        *sync.Mutex
	segmentNodes      map[string]*BreakdownNode
}

func newSegmentReporter(agent *Agent) *SegmentReporter {
	sr := &SegmentReporter{
		agent:             agent,
		reportingStrategy: nil,
		recordLock:        &sync.Mutex{},
		segmentNodes:      make(map[string]*BreakdownNode),
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

	segmentNode, exists := sr.segmentNodes[name]
	if !exists {
		segmentNode = newBreakdownNode(name)
		sr.segmentNodes[name] = segmentNode
	}

	segmentNode.updateP95(float64(duration))

	sr.recordLock.Unlock()
}

func (sr *SegmentReporter) report(trigger string) {
	sr.recordLock.Lock()

	for _, segmentNode := range sr.segmentNodes {
		segmentRoot := newBreakdownNode("root")
		segmentRoot.addChild(segmentNode)
		segmentRoot.evaluateP95()
		segmentRoot.propagate()

		metric := newMetric(sr.agent, TypeTrace, CategorySegmentTrace, segmentNode.name, UnitMillisecond)
		metric.createMeasurement(trigger, segmentRoot.measurement, segmentRoot)
		sr.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	sr.segmentNodes = make(map[string]*BreakdownNode)

	sr.recordLock.Unlock()
}
