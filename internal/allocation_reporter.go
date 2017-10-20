package internal

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"

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
	ReportInterval int64

	agent       *Agent
	started     *Flag
	reportTimer *Timer

	profileLock           *sync.Mutex
	profileStartTimestamp int64
}

func newAllocationReporter(agent *Agent) *AllocationReporter {
	ar := &AllocationReporter{
		ReportInterval: 120,

		agent:       agent,
		started:     &Flag{},
		reportTimer: nil,

		profileLock:           &sync.Mutex{},
		profileStartTimestamp: 0,
	}

	return ar
}

func (ar *AllocationReporter) start() {
	if !ar.started.SetIfUnset() {
		return
	}

	ar.profileLock.Lock()
	defer ar.profileLock.Unlock()

	ar.reset()

	if ar.agent.AutoProfiling {
		ar.reportTimer = ar.agent.createTimer(0, time.Duration(ar.ReportInterval)*time.Second, func() {
			ar.report()
		})
	}
}

func (ar *AllocationReporter) stop() {
	if !ar.started.UnsetIfSet() {
		return
	}

	if ar.reportTimer != nil {
		ar.reportTimer.Stop()
	}
}

func (ar *AllocationReporter) reset() {
	ar.profileStartTimestamp = time.Now().Unix()
}

func (ar *AllocationReporter) report() {
	if !ar.started.IsSet() {
		return
	}

	ar.profileLock.Lock()
	defer ar.profileLock.Unlock()

	if !ar.agent.AutoProfiling && ar.profileStartTimestamp > time.Now().Unix()-ar.ReportInterval {
		return
	}

	ar.agent.log("Allocation profiler: reporting profile.")

	p, e := ar.readHeapProfile()
	if e != nil {
		ar.agent.error(e)
		return
	}
	if p == nil {
		return
	}

	// allocated size
	if callGraph, err := ar.createAllocationCallGraph(p); err != nil {
		ar.agent.error(err)
	} else {
		callGraph.propagate()
		// filter calls with lower than 10KB
		callGraph.filter(2, 10000, math.Inf(0))

		metric := newMetric(ar.agent, TypeProfile, CategoryMemoryProfile, NameHeapAllocation, UnitByte)
		metric.createMeasurement(TriggerTimer, callGraph.measurement, 0, callGraph)
		ar.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	ar.reset()
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
		if !ar.agent.ProfileAgent && isAgentStack(s) {
			continue
		}

		value := s.Value[inuseSpaceTypeIndex]
		count := s.Value[inuseObjectsTypeIndex]
		if value == 0 {
			continue
		}

		currentNode := rootNode
		for i := len(s.Location) - 1; i >= 0; i-- {
			l := s.Location[i]
			funcName, fileName, fileLine := readFuncInfo(l)

			if funcName == "runtime.goexit" {
				continue
			}

			frameName := fmt.Sprintf("%v (%v:%v)", funcName, fileName, fileLine)
			currentNode = currentNode.findOrAddChild(frameName)
		}
		currentNode.increment(float64(value), int64(count))
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
