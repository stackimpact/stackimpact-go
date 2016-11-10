package internal

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"runtime"
	"runtime/trace"
	"strings"
	"time"

	pprofTrace "github.com/stackimpact/stackimpact-go/internal/pprof/trace"
)

type filterFuncType func(funcName string) bool
type stackMatcherType func(stk []*pprofTrace.Frame) bool

type Record struct {
	stk  []*pprofTrace.Frame
	n    uint64
	time int64
}

type BlockReporter struct {
	agent             *Agent
	reportingStrategy *ReportingStrategy
}

func newBlockReporter(agent *Agent) *BlockReporter {
	br := &BlockReporter{
		agent:             agent,
		reportingStrategy: nil,
	}

	br.reportingStrategy = newReportingStrategy(agent, 30, 300,
		func() float64 {
			return float64(runtime.NumGoroutine())
		},
		func(trigger string) {
			br.agent.log("Trace report triggered by reporting strategy, trigger=%v", trigger)
			br.report(trigger)
		},
	)

	return br
}

func (br *BlockReporter) start() {
	br.reportingStrategy.start()
}

func (br *BlockReporter) report(trigger string) {
	if br.agent.disableProfiling {
		return
	}

	var selectedEvents []*pprofTrace.Event
	var filterFunc filterFuncType
	duration := int64(5000)

	br.agent.log("Starting trace profiler for %v milliseconds...", duration)
	events := br.readTraceEvents(duration)
	br.agent.log("Trace profiler stopped.")

	// channels
	selectedEvents = selectEventsByType(events, pprofTrace.EvGoBlockRecv)
	if callGraph, err := br.createBlockCallGraph(selectedEvents, nil, duration); err != nil {
		br.agent.error(err)
	} else {
		// filter calls with lower than 1ms waiting time
		callGraph.filter(1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategoryChannelProfile, NameChannelWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	// network
	selectedEvents = selectEventsByType(events, pprofTrace.EvGoBlockNet)
	filterFunc = func(funcName string) bool {
		return !strings.Contains(funcName, "AcceptTCP")
	}
	if callGraph, err := br.createBlockCallGraph(selectedEvents, filterFunc, duration); err != nil {
		br.agent.error(err)
	} else {
		// filter calls with lower than 1ms waiting time
		callGraph.filter(1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategoryNetworkProfile, NameNetworkWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	// system
	selectedEvents = selectEventsByType(events, pprofTrace.EvGoSysCall)
	if callGraph, err := br.createBlockCallGraph(selectedEvents, nil, duration); err != nil {
		br.agent.error(err)
	} else {
		// filter calls with lower than 1ms waiting time
		callGraph.filter(1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategorySystemProfile, NameSystemWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	// locks
	selectedEvents = selectEventsByType(events, pprofTrace.EvGoBlockSync)
	if callGraph, err := br.createBlockCallGraph(selectedEvents, nil, duration); err != nil {
		br.agent.error(err)
	} else {
		// filter calls with lower than 1ms waiting time
		callGraph.filter(1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategoryLockProfile, NameLockWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	// HTTP handler traces
	var httpHandlerMatcher = func(stk []*pprofTrace.Frame) bool {
		return (stk[len(stk)-1].Fn == "net/http.(*conn).serve" &&
			stk[len(stk)-2].Fn == "net/http.serverHandler.ServeHTTP")
	}
	segments := br.findTopSegments(events, httpHandlerMatcher)

	if len(segments.children) > 0 {
		segments.measurement = segments.maxChild().measurement

		metric := newMetric(br.agent, TypeTrace, CategoryHTTPHandlerTrace, NameHTTPTransactions, UnitMillisecond)
		metric.createMeasurement(trigger, segments.measurement, segments)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	// HTTP client traces
	var httpClientMatcher = func(stk []*pprofTrace.Frame) bool {
		for _, f := range stk {
			if f.Fn == "net/http.(*Client).send" {
				return true
			}
		}

		return false
	}
	segments = br.findTopSegments(events, httpClientMatcher)

	if len(segments.children) > 0 {
		segments.measurement = segments.maxChild().measurement

		metric := newMetric(br.agent, TypeTrace, CategoryHTTPClientTrace, NameHTTPCalls, UnitMillisecond)
		metric.createMeasurement(trigger, segments.measurement, segments)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}

func (br *BlockReporter) findTopSegments(events []*pprofTrace.Event, stackMatcher stackMatcherType) *BreakdownNode {
	segments := newBreakdownNode("root")

	var selectedEvents []*pprofTrace.Event
	eventIndex := -1
	for i := 0; i < 2500; i++ {
		selectedEvents, eventIndex = selectEventsByStack(events, eventIndex+1, stackMatcher)
		if eventIndex == -1 {
			break
		}

		if callGraph, err := br.createBlockCallGraph(selectedEvents, nil, 1000); err != nil {
			br.agent.error(err)
			break
		} else {
			callGraph.name = fmt.Sprintf("[Segment %v]", selectedEvents[0].Ts)

			// filter calls with lower than 1ms waiting time
			callGraph.filter(1, math.Inf(0))

			br.appendSegment(segments, callGraph, 10)
		}
	}

	return segments
}

func selectEventsByStack(events []*pprofTrace.Event, startIndex int, stackMatcher stackMatcherType) ([]*pprofTrace.Event, int) {
	selected := make([]*pprofTrace.Event, 0)

	events = events[startIndex:]
	for i, ev := range events {
		j := 0
		lev := ev
		for j < 25 && lev != nil {
			j++

			if ev.StkID != 0 && len(lev.Stk) >= 2 {
				if stackMatcher(lev.Stk) {
					switch ev.Type {
					case
						pprofTrace.EvGoBlockNet,
						pprofTrace.EvGoSysCall,
						pprofTrace.EvGoBlockSend,
						pprofTrace.EvGoBlockRecv,
						pprofTrace.EvGoBlockSelect,
						pprofTrace.EvGoBlockSync,
						pprofTrace.EvGoBlockCond,
						pprofTrace.EvGoSleep:
						if ev.StkID != 0 && len(ev.Stk) > 0 {
							selected = append(selected, ev)
						}
					}
				}
			}

			if len(selected) > 0 {
				return selected, startIndex + i
			}
		}

		lev = lev.Link
	}

	return selected, -1
}

func (br *BlockReporter) appendSegment(segments *BreakdownNode, callGraph *BreakdownNode, max int) {
	if len(segments.children) < max {
		segments.addChild(callGraph)
	} else {
		minChild := segments.minChild()
		if minChild.measurement < callGraph.measurement {
			segments.removeChild(minChild)
			segments.addChild(callGraph)
		}
	}
}

func selectEventsByType(events []*pprofTrace.Event, eventType byte) []*pprofTrace.Event {
	selected := make([]*pprofTrace.Event, 0)
	for _, ev := range events {
		if ev.Type == eventType {
			selected = append(selected, ev)
		}
	}
	return selected
}

func (br *BlockReporter) createBlockCallGraph(
	events []*pprofTrace.Event,
	filterFunc filterFuncType,
	duration int64) (*BreakdownNode, error) {
	seconds := int64(duration / 1000)

	prof := make(map[uint64]Record)
	for _, ev := range events {
		if ev.Link == nil || ev.StkID == 0 || len(ev.Stk) == 0 {
			continue
		}

		rec := prof[ev.StkID]
		rec.stk = ev.Stk
		rec.n++
		rec.time += ev.Link.Ts - ev.Ts
		prof[ev.StkID] = rec
	}

	// build call graph
	rootNode := newBreakdownNode("root")

	for _, rec := range prof {
		// filter stacks
		if filterFunc != nil {
			filter := false

			for _, f := range rec.stk {
				if !filterFunc(f.Fn) {
					filter = true
				}
			}

			if filter {
				continue
			}
		}

		rootNode.measurement += float64(rec.time / 1e6 / seconds)

		parentNode := rootNode

		for i := len(rec.stk) - 1; i >= 0; i-- {
			f := rec.stk[i]

			if f.Fn == "runtime.goexit" {
				continue
			}

			frameName := fmt.Sprintf("%v (%v:%v)", f.Fn, f.File, f.Line)
			child := parentNode.findOrAddChild(frameName)
			child.measurement += float64(rec.time / 1e6 / seconds)

			parentNode = child
		}
	}

	return rootNode, nil
}

func (br *BlockReporter) readTraceEvents(duration int64) []*pprofTrace.Event {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	trace.Start(w)

	done := make(chan []*pprofTrace.Event)

	timer := time.NewTimer(time.Duration(duration) * time.Millisecond)
	go func() {
		ph := br.agent.panicHandler()
		defer ph()

		<-timer.C

		trace.Stop()

		w.Flush()
		r := bufio.NewReader(&buf)

		events, err := pprofTrace.Parse(r, "")
		if err != nil {
			br.agent.log("Cannot parse trace profile:")
			br.agent.error(err)
			done <- nil
			return
		}

		err = symbolizeEvents(events)
		if err != nil {
			br.agent.log("Error parsing trace profile:")
			br.agent.error(err)
			done <- nil
			return
		}

		done <- events
	}()

	return <-done
}

func symbolizeEvents(events []*pprofTrace.Event) error {
	pcs := make(map[uint64]*pprofTrace.Frame)
	for _, ev := range events {
		for _, f := range ev.Stk {
			if _, exists := pcs[f.PC]; !exists {
				pcs[f.PC] = &pprofTrace.Frame{PC: f.PC}
			}
		}
	}

	for _, f := range pcs {
		if fn := runtime.FuncForPC(uintptr(f.PC)); fn != nil {
			f.Fn = fn.Name()
			fileName, lineNumber := fn.FileLine(uintptr(f.PC))
			f.File = fileName
			f.Line = lineNumber
		}
	}

	for _, ev := range events {
		for i, f := range ev.Stk {
			ev.Stk[i] = pcs[f.PC]
		}
	}

	return nil
}
