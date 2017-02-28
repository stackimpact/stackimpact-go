package internal

import (
	"sync"
)

type SegmentReporter struct {
	agent             *Agent
	reportingStrategy *ReportingStrategy
	segmentNodes      map[string]*BreakdownNode
	recordLock        *sync.RWMutex
}

func newSegmentReporter(agent *Agent) *SegmentReporter {
	sr := &SegmentReporter{
		agent:             agent,
		reportingStrategy: nil,
		segmentNodes:      make(map[string]*BreakdownNode),
		recordLock:        &sync.RWMutex{},
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

func (sr *SegmentReporter) recordSegment(name string, duration float64) {
	if name == "" {
		sr.agent.log("Empty segment name")
		return
	}

	// Segment exists for the current interval.
	sr.recordLock.RLock()
	segmentNode, exists := sr.segmentNodes[name]
	if exists {
		segmentNode.updateP95(duration)
	}
	sr.recordLock.RUnlock()

	// Segment does not exist yet for the current interval.
	if !exists {
		sr.recordLock.Lock()
		segmentNode, exists := sr.segmentNodes[name]
		if !exists {
			// If segment was not created by other recordSegment call between locks, create it.
			segmentNode = newBreakdownNode(name)
			sr.segmentNodes[name] = segmentNode
		}
		sr.recordLock.Unlock()

		sr.recordLock.RLock()
		segmentNode.updateP95(duration)
		sr.recordLock.RUnlock()
	}
}

func (sr *SegmentReporter) report(trigger string) {
	sr.recordLock.Lock()
	outgoing := sr.segmentNodes
	sr.segmentNodes = make(map[string]*BreakdownNode)
	sr.recordLock.Unlock()

	for _, segmentNode := range outgoing {
		segmentRoot := newBreakdownNode("root")
		segmentRoot.addChild(segmentNode)
		segmentRoot.evaluateP95()
		segmentRoot.propagate()

		metric := newMetric(sr.agent, TypeTrace, CategorySegmentTrace, segmentNode.name, UnitMillisecond)
		metric.createMeasurement(trigger, segmentRoot.measurement, segmentRoot)
		sr.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}
