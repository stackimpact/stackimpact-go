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
	agent           *Agent
	profilerTrigger *ProfilerTrigger
}

func newAllocationReporter(agent *Agent) *AllocationReporter {
	ar := &AllocationReporter{
		agent:           agent,
		profilerTrigger: nil,
	}

	ar.profilerTrigger = newProfilerTrigger(agent, 30, 300, nil,
		func(trigger string) {
			ar.agent.log("Allocation report triggered by reporting strategy, trigger=%v", trigger)
			ar.report(trigger)
		},
	)

	return ar
}

func (ar *AllocationReporter) start() {
	ar.profilerTrigger.start()
}

func (ar *AllocationReporter) report(trigger string) {
	if ar.agent.config.isProfilingDisabled() {
		return
	}

	ar.agent.log("Reading heap profile...")
	p, e := ar.readHeapProfile()
	if e != nil {
		ar.agent.error(e)
		return
	}
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
	inuseSpaceTypeIndex := -1
	for i, s := range p.SampleType {
		if s.Type == "inuse_space" {
			inuseSpaceTypeIndex = i
			break
		}
	}

	// find "inuse_space" type index
	inuseObjectsTypeIndex := -1
	for i, s := range p.SampleType {
		if s.Type == "inuse_objects" {
			inuseObjectsTypeIndex = i
			break
		}
	}

	if inuseSpaceTypeIndex == -1 || inuseObjectsTypeIndex == -1 {
		return nil, errors.New("Unrecognized profile data")
	}

	// build call graph
	rootNode := newBreakdownNode("root")

	for _, s := range p.Sample {
		l := len(s.Value)
		if inuseSpaceTypeIndex >= l || inuseObjectsTypeIndex >= l {
			ar.agent.log("Possible inconsistence in profile types and measurements")
			continue
		}

		value := s.Value[inuseSpaceTypeIndex]
		count := s.Value[inuseObjectsTypeIndex]
		if value == 0 {
			continue
		}
		rootNode.increment(float64(value), int64(count))

		currentNode := rootNode
		for i := len(s.Location) - 1; i >= 0; i-- {
			l := s.Location[i]
			funcName, fileName, fileLine := readFuncInfo(l)

			if funcName == "runtime.goexit" {
				continue
			}

			frameName := fmt.Sprintf("%v (%v:%v)", funcName, fileName, fileLine)
			currentNode = currentNode.findOrAddChild(frameName)
			currentNode.increment(float64(value), int64(count))
		}
	}

	return rootNode, nil
}

func (ar *AllocationReporter) readHeapProfile() (*profile.Profile, error) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	err := pprof.WriteHeapProfile(w)
	if err != nil {
		return nil, err
	}

	w.Flush()
	r := bufio.NewReader(&buf)

	if p, perr := profile.Parse(r); perr == nil {
		if serr := symbolizeProfile(p); serr != nil {
			return nil, serr
		}

		if verr := p.CheckValid(); verr != nil {
			return nil, verr
		}

		return p, nil
	} else {
		return nil, perr
	}
}
