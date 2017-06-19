package internal

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

type ErrorReporter struct {
	agent       *Agent
	recordLock  *sync.RWMutex
	errorGraphs map[string]*BreakdownNode
}

func newErrorReporter(agent *Agent) *ErrorReporter {
	er := &ErrorReporter{
		agent:       agent,
		recordLock:  &sync.RWMutex{},
		errorGraphs: make(map[string]*BreakdownNode),
	}

	return er
}

func (er *ErrorReporter) start() {
	reportTicker := time.NewTicker(60 * time.Second)
	go func() {
		defer er.agent.recoverAndLog()

		for {
			select {
			case <-reportTicker.C:
				er.report()
			}
		}
	}()
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

func (er *ErrorReporter) incrementError(group string, errorGraph *BreakdownNode, err error, frames []string) {
	currentNode := errorGraph
	currentNode.increment(1, 0)
	for i := len(frames) - 1; i >= 0; i-- {
		f := frames[i]
		currentNode = currentNode.findOrAddChild(f)
		currentNode.increment(1, 0)
	}

	message := err.Error()
	if message == "" {
		message = "Undefined"
	}
	messageNode := currentNode.findChild(message)
	if messageNode == nil {
		if len(currentNode.children) < 5 {
			messageNode = currentNode.findOrAddChild(message)
		} else {
			messageNode = currentNode.findOrAddChild("Other")
		}
	}
	messageNode.increment(1, 0)
}

func (er *ErrorReporter) recordError(group string, err error, skip int) {
	frames := callerFrames(skip + 1)

	if err == nil {
		er.agent.log("Missing error object")
		return
	}

	// Error graph exists for the current interval.
	er.recordLock.RLock()
	errorGraph, exists := er.errorGraphs[group]
	if exists {
		er.incrementError(group, errorGraph, err, frames)
	}
	er.recordLock.RUnlock()

	// Error graph does not exist yet for the current interval.
	if !exists {
		er.recordLock.Lock()
		errorGraph, exists := er.errorGraphs[group]
		if !exists {
			// If segment was not created by other recordError call between locks, create it.
			errorGraph = newBreakdownNode(group)
			er.errorGraphs[group] = errorGraph
		}
		er.recordLock.Unlock()

		er.recordLock.RLock()
		er.incrementError(group, errorGraph, err, frames)
		er.recordLock.RUnlock()
	}
}

func (er *ErrorReporter) report() {
	er.recordLock.Lock()
	outgoing := er.errorGraphs
	er.errorGraphs = make(map[string]*BreakdownNode)
	er.recordLock.Unlock()

	for _, errorGraph := range outgoing {
		metric := newMetric(er.agent, TypeState, CategoryErrorProfile, errorGraph.name, UnitNone)
		metric.createMeasurement(TriggerTimer, errorGraph.measurement, 60, errorGraph)
		er.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}
