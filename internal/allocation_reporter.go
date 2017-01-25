package internal

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"runtime"
	"runtime/pprof"

	"github.com/stackimpact/stackimpact-go/internal/pprof/profile"
)

type recordSorter []runtime.MemProfileRecord

func (x recordSorter) Len() int {
	return len(x)
}

func (x recordSorter) Swap(i, j int) {
	x[i], x[j] = x[j], x[i]
}

func (x recordSorter) Less(i, j int) bool {
	return x[i].InUseBytes() > x[j].InUseBytes()
}

func readMemAlloc() float64 {
	memStats := &runtime.MemStats{}
	runtime.ReadMemStats(memStats)
	return float64(memStats.Alloc)
}

type AllocationReporter struct {
	agent             *Agent
	reportingStrategy *ReportingStrategy
}

func newAllocationReporter(agent *Agent) *AllocationReporter {
	ar := &AllocationReporter{
		agent:             agent,
		reportingStrategy: nil,
	}

	ar.reportingStrategy = newReportingStrategy(agent, 30, 300,
		func() float64 {
			memAlloc := readMemAlloc()
			return float64(int64(memAlloc / 1e6))
		},
		func(trigger string) {
			ar.agent.log("Allocation report triggered by reporting strategy, trigger=%v", trigger)
			ar.report(trigger)
		},
	)

	return ar
}

func (ar *AllocationReporter) start() {
	ar.reportingStrategy.start()
}

func (ar *AllocationReporter) report(trigger string) {
	if ar.agent.config.isProfilingDisabled() {
		return
	}

	ar.agent.log("Reading heap profile...")
	p := ar.readHeapProfile()
	if p == nil {
		return
	}
	ar.agent.log("Done.")

	// allocated size
	if callGraph, err := ar.createAllocationCallGraph(p); err != nil {
		ar.agent.error(err)
	} else {
		// filter calls with lower than 10KB
		callGraph.filter(2, 10000, math.Inf(0))

		metric := newMetric(ar.agent, TypeProfile, CategoryMemoryProfile, NameHeapAllocation, UnitByte)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		ar.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}

func (ar *AllocationReporter) createAllocationCallGraph(p *profile.Profile) (*BreakdownNode, error) {
	// find "inuse_space" type index
	typeIndex := -1
	for i, s := range p.SampleType {
		if s.Type == "inuse_space" {
			typeIndex = i

			break
		}
	}

	if typeIndex == -1 {
		return nil, errors.New("Unrecognized profile data")
	}

	// build call graph
	rootNode := newBreakdownNode("root")

	for _, s := range p.Sample {
		if len(s.Value) <= typeIndex {
			ar.agent.log("Possible inconsistence in profile types and measurements")
			continue
		}

		value := s.Value[typeIndex]
		if value == 0 {
			continue
		}
		rootNode.measurement += float64(value)

		currentNode := rootNode
		for i := len(s.Location) - 1; i >= 0; i-- {
			l := s.Location[i]
			funcName, fileName, fileLine := readFuncInfo(l)

			if funcName == "runtime.goexit" {
				continue
			}

			frameName := fmt.Sprintf("%v (%v:%v)", funcName, fileName, fileLine)
			currentNode = currentNode.findOrAddChild(frameName)
			currentNode.measurement += float64(value)
		}
	}

	return rootNode, nil
}

func (ar *AllocationReporter) readHeapProfile() *profile.Profile {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	runtime.GC()
	pprof.WriteHeapProfile(w)

	w.Flush()
	r := bufio.NewReader(&buf)

	if p, perr := profile.Parse(r); perr == nil {
		if serr := symbolizeProfile(p); serr != nil {
			ar.agent.log("Cannot symbolize heap profile:")
			ar.agent.error(serr)
			return nil
		}

		if verr := p.CheckValid(); verr != nil {
			ar.agent.log("Parsed invalid heap profile:")
			ar.agent.error(verr)
			return nil
		}

		return p
	} else {
		ar.agent.log("Error parsing heap profile:")
		ar.agent.error(perr)
		return nil
	}
}
