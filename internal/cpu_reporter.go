package internal

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/stackimpact/stackimpact-go/internal/pprof/profile"
)

type CPUReporter struct {
	agent         *Agent
	reportTrigger *ReportTrigger
}

func newCPUReporter(agent *Agent) *CPUReporter {
	cr := &CPUReporter{
		agent:         agent,
		reportTrigger: nil,
	}

	baseCpuTime, _ := readCPUTime()
	cr.reportTrigger = newReportTrigger(agent, 45, 300,
		func() map[string]float64 {
			cpuTime, _ := readCPUTime()
			cpuUsage := float64(cpuTime - baseCpuTime)
			baseCpuTime = cpuTime
			return map[string]float64{"cpu-usage": cpuUsage}
		},
		func(trigger string) {
			cr.agent.log("CPU report triggered by reporting strategy, trigger=%v", trigger)
			cr.report(trigger)
		},
	)

	return cr
}

func (cr *CPUReporter) start() {
	cr.reportTrigger.start()
}

func (cr *CPUReporter) report(trigger string) {
	if cr.agent.config.isProfilingDisabled() {
		return
	}

	cr.agent.log("Starting CPU profiler for 5000 milliseconds...")
	p, e := cr.readCPUProfile(5000)
	if e != nil {
		cr.agent.error(e)
		return
	}
	if p == nil {
		return
	}
	cr.agent.log("CPU profiler stopped.")

	if callGraph, err := cr.createCPUCallGraph(p); err != nil {
		cr.agent.error(err)
	} else {
		// filter calls with lower than 1% CPU stake
		callGraph.filter(2, 1, 100)

		metric := newMetric(cr.agent, TypeProfile, CategoryCPUProfile, NameCPUUsage, UnitPercent)
		metric.createMeasurement(trigger, callGraph.measurement, callGraph)
		cr.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}

func (cr *CPUReporter) createCPUCallGraph(p *profile.Profile) (*BreakdownNode, error) {
	// find "samples" type index
	typeIndex := -1
	for i, s := range p.SampleType {
		if s.Type == "samples" {
			typeIndex = i

			break
		}
	}

	if typeIndex == -1 {
		return nil, errors.New("Unrecognized profile data")
	}

	// calculate total possible samples
	var maxSamples int64
	if pt := p.PeriodType; pt != nil && pt.Type == "cpu" && pt.Unit == "nanoseconds" {
		maxSamples = (p.DurationNanos / p.Period) * int64(runtime.NumCPU())
	} else {
		return nil, errors.New("No period information in profile")
	}

	// build call graph
	rootNode := newBreakdownNode("root")

	for _, s := range p.Sample {
		if len(s.Value) <= typeIndex {
			cr.agent.log("Possible inconsistence in profile types and measurements")
			continue
		}

		stackSamples := s.Value[typeIndex]
		stackPercent := float64(stackSamples) / float64(maxSamples) * 100
		rootNode.increment(stackPercent, stackSamples)

		currentNode := rootNode
		for i := len(s.Location) - 1; i >= 0; i-- {
			l := s.Location[i]
			funcName, fileName, fileLine := readFuncInfo(l)

			if funcName == "runtime.goexit" {
				continue
			}

			frameName := fmt.Sprintf("%v (%v:%v)", funcName, fileName, fileLine)
			currentNode = currentNode.findOrAddChild(frameName)
			currentNode.increment(stackPercent, stackSamples)
		}
	}

	return rootNode, nil
}

func (cr *CPUReporter) readCPUProfile(duration int64) (*profile.Profile, error) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	start := time.Now()

	err := pprof.StartCPUProfile(w)
	if err != nil {
		return nil, err
	}

	done := make(chan bool)
	timer := time.NewTimer(time.Duration(duration) * time.Millisecond)
	go func() {
		defer cr.agent.recoverAndLog()

		<-timer.C

		pprof.StopCPUProfile()

		done <- true
	}()
	<-done

	w.Flush()
	r := bufio.NewReader(&buf)

	if p, perr := profile.Parse(r); perr == nil {
		if p.TimeNanos == 0 {
			p.TimeNanos = start.UnixNano()
		}
		if p.DurationNanos == 0 {
			p.DurationNanos = duration * 1e6
		}

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

func symbolizeProfile(p *profile.Profile) error {
	functions := make(map[string]*profile.Function)

	for _, l := range p.Location {
		if l.Address != 0 && len(l.Line) == 0 {
			if f := runtime.FuncForPC(uintptr(l.Address)); f != nil {
				name := f.Name()
				fileName, lineNumber := f.FileLine(uintptr(l.Address))

				pf := functions[name]
				if pf == nil {
					pf = &profile.Function{
						ID:         uint64(len(p.Function) + 1),
						Name:       name,
						SystemName: name,
						Filename:   fileName,
					}

					functions[name] = pf
					p.Function = append(p.Function, pf)
				}

				line := profile.Line{
					Function: pf,
					Line:     int64(lineNumber),
				}

				l.Line = []profile.Line{line}
				if l.Mapping != nil {
					l.Mapping.HasFunctions = true
					l.Mapping.HasFilenames = true
					l.Mapping.HasLineNumbers = true
				}
			}
		}
	}

	return nil
}

func readFuncInfo(l *profile.Location) (funcName string, fileName string, fileLine int64) {
	for li := range l.Line {
		if fn := l.Line[li].Function; fn != nil {
			return fn.Name, fn.Filename, l.Line[li].Line
		}
	}

	return "", "", 0
}
