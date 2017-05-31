package internal

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"regexp"
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
	agent           *Agent
	profilerTrigger *ProfilerTrigger
}

func newBlockReporter(agent *Agent) *BlockReporter {
	br := &BlockReporter{
		agent:           agent,
		profilerTrigger: nil,
	}

	br.profilerTrigger = newProfilerTrigger(agent, 75, 300,
		func() map[string]float64 {
			metrics := map[string]float64{"num-goroutines": float64(runtime.NumGoroutine())}

			segmentDurations := br.agent.segmentReporter.readLastDurations()
			for name, duration := range segmentDurations {
				metrics[name] = duration
			}

			return metrics
		},
		func(trigger string) {
			br.agent.log("Trace report triggered by reporting strategy, trigger=%v", trigger)
			br.report(trigger)
		},
	)

	return br
}

func (br *BlockReporter) start() {
	br.profilerTrigger.start()
}

func (br *BlockReporter) report(trigger string) {
	if br.agent.config.isProfilingDisabled() {
		return
	}

	duration, aerr := br.estimateTraceDuration()
	if aerr != nil {
		br.agent.error(aerr)
		return
	}

	if duration < 5 {
		br.agent.log("Trace profiling duration is less than 5 milliseconds, returing.")
		return
	}

	br.agent.log("Starting trace profiler for %v milliseconds...", duration)
	events, rerr := br.readTraceEvents(duration)
	if rerr != nil {
		br.agent.error(rerr)
		return
	}

	br.agent.log("Trace profiler stopped.")

	br.reportChannelProfile(trigger, events, duration)
	br.reportNetworkProfile(trigger, events, duration)
	br.reportSystemProfile(trigger, events, duration)
	br.reportLockProfile(trigger, events, duration)
	br.reportHTTPHandlerTrace(trigger, events)
	br.reportHTTPClientTrace(trigger, events)
	br.reportSQLStatementTrace(trigger, events)
	br.reportMongoDBCallTrace(trigger, events)
	br.reportRedisCallTrace(trigger, events)
}

func (br *BlockReporter) reportChannelProfile(trigger string, events []*pprofTrace.Event, duration int64) {
	selectorFunc := func(event *pprofTrace.Event) bool {
		return (event.Type == pprofTrace.EvGoBlockRecv)
	}
	selectedEvents := selectEvents(events, selectorFunc)
	if callGraph, err := br.createBlockCallGraph(selectedEvents, nil, duration); err != nil {
		br.agent.error(err)
	} else {
		callGraph.propagate()
		callGraph.filter(2, 1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategoryChannelProfile, NameChannelWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}

func (br *BlockReporter) reportNetworkProfile(trigger string, events []*pprofTrace.Event, duration int64) {
	selectorFunc := func(event *pprofTrace.Event) bool {
		return (event.Type == pprofTrace.EvGoBlockNet)
	}
	selectedEvents := selectEvents(events, selectorFunc)
	filterFunc := func(funcName string) bool {
		return !strings.Contains(funcName, "AcceptTCP")
	}
	if callGraph, err := br.createBlockCallGraph(selectedEvents, filterFunc, duration); err != nil {
		br.agent.error(err)
	} else {
		callGraph.propagate()
		callGraph.filter(2, 1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategoryNetworkProfile, NameNetworkWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}

func (br *BlockReporter) reportSystemProfile(trigger string, events []*pprofTrace.Event, duration int64) {
	selectorFunc := func(event *pprofTrace.Event) bool {
		return (event.Type == pprofTrace.EvGoSysCall)
	}
	selectedEvents := selectEvents(events, selectorFunc)
	if callGraph, err := br.createBlockCallGraph(selectedEvents, nil, duration); err != nil {
		br.agent.error(err)
	} else {
		callGraph.propagate()
		callGraph.filter(5, 1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategorySystemProfile, NameSystemWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}

func (br *BlockReporter) reportLockProfile(trigger string, events []*pprofTrace.Event, duration int64) {
	selectorFunc := func(event *pprofTrace.Event) bool {
		return (event.Type == pprofTrace.EvGoBlockSync)
	}
	selectedEvents := selectEvents(events, selectorFunc)
	if callGraph, err := br.createBlockCallGraph(selectedEvents, nil, duration); err != nil {
		br.agent.error(err)
	} else {
		callGraph.propagate()
		callGraph.filter(2, 1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategoryLockProfile, NameLockWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

}

func (br *BlockReporter) reportHTTPHandlerTrace(trigger string, events []*pprofTrace.Event) {
	selectorFunc := func(event *pprofTrace.Event) bool {
		l := len(event.Stk)
		return (l >= 2 &&
			event.Stk[l-1].Fn == "net/http.(*conn).serve" &&
			event.Stk[l-2].Fn == "net/http.serverHandler.ServeHTTP")
	}
	selectedEvents := selectEvents(events, selectorFunc)
	if len(selectedEvents) == 0 {
		return
	}

	if callGraph, err := br.createTraceCallGraph(selectedEvents); err != nil {
		br.agent.error(err)
	} else {
		callGraph.evaluateP95()
		callGraph.propagate()
		callGraph.filter(5, 1, math.Inf(0))

		metric := newMetric(br.agent, TypeTrace, CategoryHTTPHandlerTrace, NameHTTPTransactions, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}

func (br *BlockReporter) reportHTTPClientTrace(trigger string, events []*pprofTrace.Event) {
	selectorFunc := func(event *pprofTrace.Event) bool {
		for _, f := range event.Stk {
			if f.Fn == "net/http.(*Client).send" {
				return true
			}
		}

		return false
	}
	selectedEvents := selectEvents(events, selectorFunc)
	if len(selectedEvents) == 0 {
		return
	}

	if callGraph, err := br.createTraceCallGraph(selectedEvents); err != nil {
		br.agent.error(err)
	} else {
		callGraph.evaluateP95()
		callGraph.propagate()
		callGraph.filter(5, 1, math.Inf(0))

		metric := newMetric(br.agent, TypeTrace, CategoryHTTPClientTrace, NameHTTPCalls, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}

func (br *BlockReporter) reportSQLStatementTrace(trigger string, events []*pprofTrace.Event) {
	selectorRe := regexp.MustCompile(`^database\/sql\.\(\*(DB|Stmt|Tx|Rows)\)\.[A-Z]`)
	selectorFunc := func(event *pprofTrace.Event) bool {
		for _, f := range event.Stk {
			if selectorRe.MatchString(f.Fn) {
				return true
			}
		}

		return false
	}
	selectedEvents := selectEvents(events, selectorFunc)
	if len(selectedEvents) == 0 {
		return
	}

	if callGraph, err := br.createTraceCallGraph(selectedEvents); err != nil {
		br.agent.error(err)
	} else {
		callGraph.evaluateP95()
		callGraph.propagate()
		callGraph.filter(5, 1, math.Inf(0))

		metric := newMetric(br.agent, TypeTrace, CategoryDBClientTrace, NameSQLStatements, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}

func (br *BlockReporter) reportMongoDBCallTrace(trigger string, events []*pprofTrace.Event) {
	selectorRe := regexp.MustCompile(`^gopkg\.in\/mgo%2ev2\.\(\*\w+\)\.[A-Z]`)
	selectorFunc := func(event *pprofTrace.Event) bool {
		for _, f := range event.Stk {
			if selectorRe.MatchString(f.Fn) {
				return true
			}
		}

		return false
	}
	selectedEvents := selectEvents(events, selectorFunc)
	if len(selectedEvents) == 0 {
		return
	}

	if callGraph, err := br.createTraceCallGraph(selectedEvents); err != nil {
		br.agent.error(err)
	} else {
		callGraph.evaluateP95()
		callGraph.propagate()
		callGraph.filter(5, 1, math.Inf(0))

		metric := newMetric(br.agent, TypeTrace, CategoryDBClientTrace, NameMongoDBCalls, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}

func (br *BlockReporter) reportRedisCallTrace(trigger string, events []*pprofTrace.Event) {
	redigoSelectorRe := regexp.MustCompile(`^github\.com\/garyburd\/redigo\/redis\.\(\*conn\)\.[A-Z]`)
	goredisSelectorRe := regexp.MustCompile(`^gopkg\.in\/redis%2ev5\.\(\*\w+\)\.[A-Z]`)
	selectorFunc := func(event *pprofTrace.Event) bool {
		for _, f := range event.Stk {
			if redigoSelectorRe.MatchString(f.Fn) || goredisSelectorRe.MatchString(f.Fn) {
				return true
			}
		}

		return false
	}
	selectedEvents := selectEvents(events, selectorFunc)
	if len(selectedEvents) == 0 {
		return
	}

	if callGraph, err := br.createTraceCallGraph(selectedEvents); err != nil {
		br.agent.error(err)
	} else {
		callGraph.evaluateP95()
		callGraph.propagate()
		callGraph.filter(5, 1, math.Inf(0))

		metric := newMetric(br.agent, TypeTrace, CategoryDBClientTrace, NameRedisCalls, UnitMillisecond)
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
		currentNode.increment(t, 1)
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

func (br *BlockReporter) estimateTraceDuration() (int64, error) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	err := trace.Start(w)
	if err != nil {
		return 0, err
	}

	done := make(chan bool)

	probeDuration := 10
	timer := time.NewTimer(time.Duration(probeDuration) * time.Millisecond)
	go func() {
		defer br.agent.recoverAndLog()

		<-timer.C

		trace.Stop()

		done <- true
	}()
	<-done

	w.Flush()
	r := bufio.NewReader(&buf)

	events, err := pprofTrace.Parse(r, "")
	if err != nil {
		return 0, err
	}

	size := len(events)

	// Estimate duration, which will limit max memory overhead after parsing trace log
	// to max 10MB, which is around max 20,000 events.
	// duration = (max_overhead / probe_size) * probe_duration
	br.agent.log("Probe trace log size: %v", size)
	duration := int64((20000.0 / float64(size)) * float64(probeDuration))
	if duration > 5000 {
		duration = 5000
	}

	return duration, nil
}

func (br *BlockReporter) readTraceEvents(duration int64) ([]*pprofTrace.Event, error) {
	var startMem float64
	if br.agent.Debug {
		runtime.GC()
		startMem = readMemAlloc()
	}

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	err := trace.Start(w)
	if err != nil {
		return nil, err
	}

	done := make(chan bool)
	timer := time.NewTimer(time.Duration(duration) * time.Millisecond)
	go func() {
		defer br.agent.recoverAndLog()

		<-timer.C

		trace.Stop()

		done <- true
	}()
	<-done

	w.Flush()
	r := bufio.NewReader(&buf)

	events, err := pprofTrace.Parse(r, "")
	if err != nil {
		return nil, err
	}

	br.agent.log("Trace log size: %v", len(events))
	if br.agent.Debug {
		br.agent.log("Memory overhead (bytes): %v", int64(readMemAlloc()-startMem))
	}

	selectorFunc := func(event *pprofTrace.Event) bool {
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
			return true
		default:
			return false
		}
	}
	events = selectEvents(events, selectorFunc)

	err = symbolizeEvents(events)
	if err != nil {
		return nil, err
	}

	return events, nil
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
