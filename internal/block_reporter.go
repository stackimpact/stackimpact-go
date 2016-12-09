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
type selectorFuncType func(event *pprofTrace.Event) bool

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
	var selectorFunc selectorFuncType

	duration := br.adjustTraceDuration()
	br.agent.log("Starting trace profiler for %v milliseconds...", duration)
	events := br.readTraceEvents(duration)
	br.agent.log("Trace profiler stopped.")

	// channels
	selectorFunc = func(event *pprofTrace.Event) bool {
		return (event.Type == pprofTrace.EvGoBlockRecv)
	}
	selectedEvents = selectEvents(events, selectorFunc)
	if callGraph, err := br.createBlockCallGraph(selectedEvents, nil, duration); err != nil {
		br.agent.error(err)
	} else {
		callGraph.propagate()
		callGraph.filter(1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategoryChannelProfile, NameChannelWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	// network
	selectorFunc = func(event *pprofTrace.Event) bool {
		return (event.Type == pprofTrace.EvGoBlockNet)
	}
	selectedEvents = selectEvents(events, selectorFunc)
	filterFunc = func(funcName string) bool {
		return !strings.Contains(funcName, "AcceptTCP")
	}
	if callGraph, err := br.createBlockCallGraph(selectedEvents, filterFunc, duration); err != nil {
		br.agent.error(err)
	} else {
		callGraph.propagate()
		callGraph.filter(1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategoryNetworkProfile, NameNetworkWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	// system
	selectorFunc = func(event *pprofTrace.Event) bool {
		return (event.Type == pprofTrace.EvGoSysCall)
	}
	selectedEvents = selectEvents(events, selectorFunc)
	if callGraph, err := br.createBlockCallGraph(selectedEvents, nil, duration); err != nil {
		br.agent.error(err)
	} else {
		callGraph.propagate()
		callGraph.filter(1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategorySystemProfile, NameSystemWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	// locks
	selectorFunc = func(event *pprofTrace.Event) bool {
		return (event.Type == pprofTrace.EvGoBlockSync)
	}
	selectedEvents = selectEvents(events, selectorFunc)
	if callGraph, err := br.createBlockCallGraph(selectedEvents, nil, duration); err != nil {
		br.agent.error(err)
	} else {
		callGraph.propagate()
		callGraph.filter(1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategoryLockProfile, NameLockWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	// HTTP handler traces
	selectorFunc = func(event *pprofTrace.Event) bool {
		l := len(event.Stk)

		switch event.Type {
		case
			pprofTrace.EvGoBlockNet,
			pprofTrace.EvGoSysCall,
			pprofTrace.EvGoBlockSend,
			pprofTrace.EvGoBlockRecv,
			pprofTrace.EvGoBlockSelect,
			pprofTrace.EvGoBlockSync,
			pprofTrace.EvGoBlockCond,
			pprofTrace.EvGoSleep:
		default:
			return false
		}

		return (l >= 2 &&
			event.Stk[l-1].Fn == "net/http.(*conn).serve" &&
			event.Stk[l-2].Fn == "net/http.serverHandler.ServeHTTP")
	}
	selectedEvents = selectEvents(events, selectorFunc)
	if callGraph, err := br.createTraceCallGraph(selectedEvents); err != nil {
		br.agent.error(err)
	} else {
		callGraph.evaluateP95()
		callGraph.propagate()
		callGraph.filter(1, math.Inf(0))

		metric := newMetric(br.agent, TypeTrace, CategoryHTTPHandlerTrace, NameHTTPTransactions, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	// HTTP client traces
	selectorFunc = func(event *pprofTrace.Event) bool {
		switch event.Type {
		case
			pprofTrace.EvGoBlockNet,
			pprofTrace.EvGoSysCall,
			pprofTrace.EvGoBlockSend,
			pprofTrace.EvGoBlockRecv,
			pprofTrace.EvGoBlockSelect,
			pprofTrace.EvGoBlockSync,
			pprofTrace.EvGoBlockCond,
			pprofTrace.EvGoSleep:
		default:
			return false
		}

		for _, f := range event.Stk {
			if f.Fn == "net/http.(*Client).send" {
				return true
			}
		}

		return false
	}
	selectedEvents = selectEvents(events, selectorFunc)
	if callGraph, err := br.createTraceCallGraph(selectedEvents); err != nil {
		br.agent.error(err)
	} else {
		callGraph.evaluateP95()
		callGraph.propagate()
		callGraph.filter(1, math.Inf(0))

		metric := newMetric(br.agent, TypeTrace, CategoryHTTPClientTrace, NameHTTPCalls, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}

func selectEvents(events []*pprofTrace.Event, selectorFunc selectorFuncType) []*pprofTrace.Event {
	selected := make([]*pprofTrace.Event, 0)

	for _, ev := range events {
		if ev.StkID != 0 && len(ev.Stk) > 0 {
			if selectorFunc == nil || selectorFunc(ev) {
				selected = append(selected, ev)
			}
		}
	}

	return selected
}

func (br *BlockReporter) createBlockCallGraph(
	events []*pprofTrace.Event,
	filterFunc filterFuncType,
	duration int64) (*BreakdownNode, error) {

	seconds := float64(duration) / 1000.0

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

		currentNode := rootNode
		for i := len(rec.stk) - 1; i >= 0; i-- {
			f := rec.stk[i]

			if f.Fn == "runtime.goexit" {
				continue
			}

			frameName := fmt.Sprintf("%v (%v:%v)", f.Fn, f.File, f.Line)
			currentNode = currentNode.findOrAddChild(frameName)
		}

		t := float64(rec.time) / float64(1e6) / seconds
		currentNode.measurement += t
	}

	return rootNode, nil
}

func (br *BlockReporter) createTraceCallGraph(
	events []*pprofTrace.Event) (*BreakdownNode, error) {

	// build call graph
	rootNode := newBreakdownNode("root")

	for _, ev := range events {
		if ev.Link == nil || ev.StkID == 0 || len(ev.Stk) == 0 {
			continue
		}

		currentNode := rootNode
		for i := len(ev.Stk) - 1; i >= 0; i-- {
			f := ev.Stk[i]

			if f.Fn == "runtime.goexit" {
				continue
			}

			frameName := fmt.Sprintf("%v (%v:%v)", f.Fn, f.File, f.Line)
			currentNode = currentNode.findOrAddChild(frameName)
		}

		t := float64(ev.Link.Ts-ev.Ts) / float64(1e6)
		currentNode.updateP95(t)
	}

	return rootNode, nil
}

func (br *BlockReporter) adjustTraceDuration() int64 {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	trace.Start(w)

	done := make(chan int64)

	timer := time.NewTimer(10 * time.Millisecond)
	go func() {
		defer br.agent.recoverAndLog()

		<-timer.C

		trace.Stop()

		w.Flush()
		size := buf.Len()

		// adjust tracing duration based on 10ms sample trace log size
		duration := int64(5000 - size/5)
		if duration <= 0 {
			duration = 100
		}

		done <- duration
	}()

	return <-done
}

func (br *BlockReporter) readTraceEvents(duration int64) []*pprofTrace.Event {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	trace.Start(w)

	done := make(chan []*pprofTrace.Event)

	timer := time.NewTimer(time.Duration(duration) * time.Millisecond)
	go func() {
		defer br.agent.recoverAndLog()

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
