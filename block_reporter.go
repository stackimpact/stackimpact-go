package stackimpact

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"runtime"
	"runtime/trace"
	"strings"
	"time"

	pprofTrace "github.com/stackimpact/stackimpact-go/pprof/trace"
)

type filterFuncType func(funcName string) bool

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

	duration := int64(5000)

	br.agent.log("Starting trace profiler for %v milliseconds...", duration)
	events := br.readTraceProfile(duration)
	br.agent.log("Trace profiler stopped.")

	// network
	filter := func(funcName string) bool {
		return !strings.Contains(funcName, "AcceptTCP")
	}
	if callGraph, err := br.createBlockCallGraph(events, pprofTrace.EvGoBlockNet, filter, duration); err != nil {
		br.agent.error(err)
	} else {
		// filter calls with lower than 1ms waiting time
		callGraph.filter(1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategoryNetworkProfile, NameWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	// system
	if callGraph, err := br.createBlockCallGraph(events, pprofTrace.EvGoSysCall, nil, duration); err != nil {
		br.agent.error(err)
	} else {
		// filter calls with lower than 1ms waiting time
		callGraph.filter(1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategorySystemProfile, NameWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	// locks
	if callGraph, err := br.createBlockCallGraph(events, pprofTrace.EvGoBlockSync, nil, duration); err != nil {
		br.agent.error(err)
	} else {
		// filter calls with lower than 1ms waiting time
		callGraph.filter(1, math.Inf(0))

		metric := newMetric(br.agent, TypeProfile, CategoryLockProfile, NameWaitTime, UnitMillisecond)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}

func (br *BlockReporter) createBlockCallGraph(
	events []*pprofTrace.Event,
	eventType byte,
	filterFunc filterFuncType,
	duration int64) (*BreakdownNode, error) {
	seconds := int64(duration / 1000)

	prof := make(map[uint64]Record)
	for _, ev := range events {
		if ev.Type != eventType || ev.Link == nil || ev.StkID == 0 || len(ev.Stk) == 0 {
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

func (br *BlockReporter) readTraceProfile(duration int64) []*pprofTrace.Event {
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
