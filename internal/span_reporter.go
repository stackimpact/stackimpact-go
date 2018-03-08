package internal

import (
	"sync"
	"time"
)

type SpanReporter struct {
	ReportInterval int64

	agent       *Agent
	started     *Flag
	reportTimer *Timer

	spanNodes  map[string]*BreakdownNode
	recordLock *sync.RWMutex
}

func newSpanReporter(agent *Agent) *SpanReporter {
	sr := &SpanReporter{
		ReportInterval: 60,

		agent:       agent,
		started:     &Flag{},
		reportTimer: nil,

		spanNodes:  nil,
		recordLock: &sync.RWMutex{},
	}

	return sr
}

func (sr *SpanReporter) reset() {
	sr.recordLock.Lock()
	defer sr.recordLock.Unlock()

	sr.spanNodes = make(map[string]*BreakdownNode)
}

func (sr *SpanReporter) start() {
	if !sr.agent.AutoProfiling {
		return
	}

	if !sr.started.SetIfUnset() {
		return
	}

	sr.reset()

	sr.reportTimer = sr.agent.createTimer(0, time.Duration(sr.ReportInterval)*time.Second, func() {
		sr.report()
	})
}

func (sr *SpanReporter) stop() {
	if !sr.started.UnsetIfSet() {
		return
	}

	if sr.reportTimer != nil {
		sr.reportTimer.Stop()
	}
}

func (sr *SpanReporter) recordSpan(name string, duration float64) {
	if !sr.started.IsSet() {
		return
	}

	if name == "" {
		sr.agent.log("Empty span name")
		return
	}

	// Span exists for the current interval.
	sr.recordLock.RLock()
	node, nExists := sr.spanNodes[name]
	if nExists {
		node.updateP95(duration)
	}
	sr.recordLock.RUnlock()

	// Span does not exist yet for the current interval.
	if !nExists {
		sr.recordLock.Lock()
		node, nExists := sr.spanNodes[name]
		if !nExists {
			// If span was not created by other recordSpan call between locks, create it.
			node = newBreakdownNode(name)
			sr.spanNodes[name] = node
		}
		sr.recordLock.Unlock()

		sr.recordLock.RLock()
		node.updateP95(duration)
		sr.recordLock.RUnlock()
	}
}

func (sr *SpanReporter) report() {
	if !sr.started.IsSet() {
		return
	}

	sr.recordLock.Lock()
	outgoing := sr.spanNodes
	sr.spanNodes = make(map[string]*BreakdownNode)
	sr.recordLock.Unlock()

	for _, spanNode := range outgoing {
		spanRoot := newBreakdownNode("root")
		spanRoot.addChild(spanNode)
		spanRoot.evaluateP95()
		spanRoot.propagate()

		metric := newMetric(sr.agent, TypeState, CategorySpan, spanNode.name, UnitMillisecond)
		metric.createMeasurement(TriggerTimer, spanRoot.measurement, 0, nil)
		sr.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}
