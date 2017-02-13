package internal

import (
	"fmt"
	"runtime"
	"sync"
)

type ErrorReporter struct {
	agent             *Agent
	reportingStrategy *ReportingStrategy
	recordLock        *sync.Mutex
	errorGraphs       map[string]*BreakdownNode
}

func newErrorReporter(agent *Agent) *ErrorReporter {
	er := &ErrorReporter{
		agent:             agent,
		reportingStrategy: nil,
		recordLock:        &sync.Mutex{},
		errorGraphs:       make(map[string]*BreakdownNode),
	}

	er.reportingStrategy = newReportingStrategy(agent, 60, 60, nil,
		func(trigger string) {
			er.agent.log("Error report triggered by reporting strategy, trigger=%v", trigger)
			er.report(trigger)
		},
	)

	return er
}

func (er *ErrorReporter) start() {
	er.reportingStrategy.start()
}

func callerFrames(skip int) []string {
	stack := make([]uintptr, 50)
	runtime.Callers(skip+2, stack)

	frames := make([]string, 0)
	for _, pc := range stack {
		if pc != 0 {
			if fn := runtime.FuncForPC(pc); fn != nil {
				funcName := fn.Name()

				if funcName == "runtime.goexit" {
					continue
				}

				fileName, lineNumber := fn.FileLine(pc)
				frames = append(frames, fmt.Sprintf("%v (%v:%v)", fn.Name(), fileName, lineNumber))
			}
		}
	}

	return frames
}

func (er *ErrorReporter) recordError(group string, err error, skip int) {
	go er.recordErrorSync(group, err, callerFrames(skip+1))
}

func (er *ErrorReporter) recordErrorSync(group string, err error, frames []string) {
	if err == nil {
		er.agent.log("Missing error object")
		return
	}

	er.recordLock.Lock()

	errorGraph, exists := er.errorGraphs[group]
	if !exists {
		errorGraph = newBreakdownNode(group)
		er.errorGraphs[group] = errorGraph
	}
	errorGraph.increment(1, 0)

	currentNode := errorGraph.findOrAddChild(err.Error())
	currentNode.increment(1, 0)
	for i := len(frames) - 1; i >= 0; i-- {
		f := frames[i]
		currentNode = currentNode.findOrAddChild(f)
		currentNode.increment(1, 0)
	}

	er.recordLock.Unlock()
}

func (er *ErrorReporter) report(trigger string) {
	er.recordLock.Lock()

	for _, errorGraph := range er.errorGraphs {
		metric := newMetric(er.agent, TypeState, CategoryErrorProfile, errorGraph.name, UnitNone)
		metric.createMeasurement(trigger, errorGraph.measurement, errorGraph)
		er.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	er.errorGraphs = make(map[string]*BreakdownNode)

	er.recordLock.Unlock()
}
