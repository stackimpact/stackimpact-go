package internal

import (
	"sync"
)

type SegmentReporter struct {
	agent             *Agent
	reportingStrategy *ReportingStrategy
	recordLock        *sync.Mutex
	segmentGraphs     map[string]*BreakdownNode
}

type Segment struct {
	path     []string
	duration int64
}

func newSegmentReporter(agent *Agent) *SegmentReporter {
	sr := &SegmentReporter{
		agent:             agent,
		reportingStrategy: nil,
		recordLock:        &sync.Mutex{},
		segmentGraphs:     make(map[string]*BreakdownNode),
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

func (sr *SegmentReporter) recordSegment(path []string, duration int64) {
	go sr.recordSegmentSync(path, duration)
}

func (sr *SegmentReporter) recordSegmentSync(path []string, duration int64) {
	if len(path) == 0 || len(path) > 5 {
		sr.agent.log("Invalid segment path, length=%v", len(path))
		return
	}

	sr.recordLock.Lock()

	var currentNode *BreakdownNode = nil
	for _, name := range path {
		if currentNode == nil {
			segmentGraph, exists := sr.segmentGraphs[path[0]]
			if !exists {
				segmentGraph = newBreakdownNode(path[0])
				sr.segmentGraphs[path[0]] = segmentGraph
			}

			currentNode = segmentGraph
		} else {
			currentNode = currentNode.findOrAddChild(name)
		}
	}

	currentNode.updateP95(float64(duration))

	sr.recordLock.Unlock()
}

func (sr *SegmentReporter) report(trigger string) {
	sr.recordLock.Lock()

	for _, segmentGraph := range sr.segmentGraphs {
		segmentGraph.evaluateP95()

		if len(segmentGraph.children) > 0 {
			metric := newMetric(sr.agent, TypeTrace, CategorySegmentTrace, segmentGraph.name, UnitMillisecond)
			metric.createMeasurement(trigger, segmentGraph.measurement, segmentGraph)
			sr.agent.messageQueue.addMessage("metric", metric.toMap())
		}

		metric := newMetric(sr.agent, TypeState, CategorySegments, segmentGraph.name, UnitMillisecond)
		metric.createMeasurement(trigger, segmentGraph.measurement, nil)
		sr.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	sr.segmentGraphs = make(map[string]*BreakdownNode)

	sr.recordLock.Unlock()
}
