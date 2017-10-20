package internal

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	"github.com/stackimpact/stackimpact-go/internal/pprof/profile"
)

type CPUReporter struct {
	MaxProfileDuration int64
	MaxSpanDuration    int64
	MaxSpanCount       int32
	SpanInterval       int64
	ReportInterval     int64

	agent       *Agent
	started     *Flag
	spanTimer   *Timer
	reportTimer *Timer

	profile               *BreakdownNode
	profileLock           *sync.Mutex
	profileStartTimestamp int64
	profileDuration       int64
	spanCount             int32
	spanActive            *Flag
	spanWriter            *bufio.Writer
	spanBuffer            *bytes.Buffer
	spanStart             int64
	spanTimeout           *Timer
}

func newCPUReporter(agent *Agent) *CPUReporter {
	cr := &CPUReporter{
		MaxProfileDuration: 20,
		MaxSpanDuration:    2,
		MaxSpanCount:       30,
		SpanInterval:       8,
		ReportInterval:     120,

		agent:       agent,
		started:     &Flag{},
		spanTimer:   nil,
		reportTimer: nil,

		profile:               nil,
		profileLock:           &sync.Mutex{},
		profileStartTimestamp: 0,
		profileDuration:       0,
		spanCount:             0,
		spanActive:            &Flag{},
		spanWriter:            nil,
		spanBuffer:            nil,
		spanStart:             0,
		spanTimeout:           nil,
	}

	return cr
}

func (cr *CPUReporter) start() {
	if !cr.started.SetIfUnset() {
		return
	}

	cr.profileLock.Lock()
	defer cr.profileLock.Unlock()

	cr.reset()

	if cr.agent.AutoProfiling {
		cr.spanTimer = cr.agent.createTimer(0, time.Duration(cr.SpanInterval)*time.Second, func() {
			time.Sleep(time.Duration(rand.Int63n(cr.SpanInterval-cr.MaxSpanDuration)) * time.Second)
			cr.startProfiling(false)
		})

		cr.reportTimer = cr.agent.createTimer(0, time.Duration(cr.ReportInterval)*time.Second, func() {
			cr.report()
		})
	}
}

func (cr *CPUReporter) stop() {
	if !cr.started.UnsetIfSet() {
		return
	}

	if cr.spanTimer != nil {
		cr.spanTimer.Stop()
	}

	if cr.reportTimer != nil {
		cr.reportTimer.Stop()
	}
}

func (cr *CPUReporter) reset() {
	cr.profile = newBreakdownNode("root")
	cr.profileStartTimestamp = time.Now().Unix()
	cr.profileDuration = 0
	cr.spanCount = 0
}

func (cr *CPUReporter) startProfiling(rateLimit bool) bool {
	if !cr.started.IsSet() {
		return false
	}

	cr.profileLock.Lock()
	defer cr.profileLock.Unlock()

	if cr.profileDuration > cr.MaxProfileDuration*1e9 {
		cr.agent.log("CPU profiler: max profiling duration reached.")
		return false
	}

	if rateLimit && cr.spanCount > cr.MaxSpanCount {
		cr.agent.log("CPU profiler: max profiling span count reached.")
		return false
	}

	if !cr.agent.profilerActive.SetIfUnset() {
		cr.agent.log("CPU profiler: another profiler currently active.")
		return false
	}

	cr.agent.log("Starting CPU profiler.")

	err := cr.startCPUProfiler()
	if err != nil {
		cr.agent.profilerActive.Unset()
		cr.agent.error(err)
		return false
	}

	cr.spanTimeout = cr.agent.createTimer(time.Duration(cr.MaxSpanDuration)*time.Second, 0, func() {
		cr.stopProfiling()
	})

	cr.spanCount++
	cr.spanActive.Set()
	cr.spanStart = time.Now().UnixNano()

	return true
}

func (cr *CPUReporter) stopProfiling() {
	cr.profileLock.Lock()
	defer cr.profileLock.Unlock()

	if !cr.spanActive.UnsetIfSet() {
		return
	}
	cr.spanTimeout.Stop()

	defer cr.agent.profilerActive.Unset()

	p, err := cr.stopCPUProfiler()
	if err != nil {
		cr.agent.error(err)
		return
	}
	if p == nil {
		return
	}
	cr.agent.log("CPU profiler stopped.")

	if uerr := cr.updateCPUProfile(p); uerr != nil {
		cr.agent.error(uerr)
	}

	cr.profileDuration += time.Now().UnixNano() - cr.spanStart
}

func (cr *CPUReporter) report() {
	if !cr.started.IsSet() {
		return
	}

	cr.profileLock.Lock()
	defer cr.profileLock.Unlock()

	if !cr.agent.AutoProfiling && cr.profileStartTimestamp > time.Now().Unix()-cr.ReportInterval {
		return
	}

	if cr.profileDuration == 0 {
		return
	}

	cr.agent.log("CPU profiler: reporting profile.")

	cr.profile.convertToPercentage(float64(cr.profileDuration * int64(runtime.NumCPU())))
	cr.profile.propagate()
	// filter calls with lower than 1% CPU stake
	cr.profile.filter(2, 1, 100)

	metric := newMetric(cr.agent, TypeProfile, CategoryCPUProfile, NameCPUUsage, UnitPercent)
	metric.createMeasurement(TriggerTimer, cr.profile.measurement, 0, cr.profile)
	cr.agent.messageQueue.addMessage("metric", metric.toMap())

	cr.reset()
}

func (cr *CPUReporter) updateCPUProfile(p *profile.Profile) error {
	samplesIndex := -1
	cpuIndex := -1
	for i, s := range p.SampleType {
		if s.Type == "samples" {
			samplesIndex = i
		} else if s.Type == "cpu" {
			cpuIndex = i
		}
	}

	if samplesIndex == -1 || cpuIndex == -1 {
		return errors.New("Unrecognized profile data")
	}

	// build call graph
	for _, s := range p.Sample {
		if !cr.agent.ProfileAgent && isAgentStack(s) {
			continue
		}

		stackSamples := s.Value[samplesIndex]
		stackDuration := float64(s.Value[cpuIndex])

		currentNode := cr.profile
		for i := len(s.Location) - 1; i >= 0; i-- {
			l := s.Location[i]
			funcName, fileName, fileLine := readFuncInfo(l)

			if funcName == "runtime.goexit" {
				continue
			}

			frameName := fmt.Sprintf("%v (%v:%v)", funcName, fileName, fileLine)
			currentNode = currentNode.findOrAddChild(frameName)
		}

		currentNode.increment(stackDuration, stackSamples)
	}

	return nil
}

func (cr *CPUReporter) startCPUProfiler() error {
	cr.spanBuffer = &bytes.Buffer{}
	cr.spanWriter = bufio.NewWriter(cr.spanBuffer)
	cr.spanStart = time.Now().UnixNano()

	err := pprof.StartCPUProfile(cr.spanWriter)
	if err != nil {
		return err
	}

	return nil
}

func (cr *CPUReporter) stopCPUProfiler() (*profile.Profile, error) {
	pprof.StopCPUProfile()

	cr.spanWriter.Flush()
	r := bufio.NewReader(cr.spanBuffer)

	if p, perr := profile.Parse(r); perr == nil {
		cr.spanWriter = nil
		cr.spanBuffer = nil

		if p.TimeNanos == 0 {
			p.TimeNanos = cr.spanStart
		}
		if p.DurationNanos == 0 {
			p.DurationNanos = time.Now().UnixNano() - cr.spanStart
		}

		if serr := symbolizeProfile(p); serr != nil {
			return nil, serr
		}

		if verr := p.CheckValid(); verr != nil {
			return nil, verr
		}

		return p, nil
	} else {
		cr.spanWriter = nil
		cr.spanBuffer = nil

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

var agentPath = filepath.Join("github.com", "stackimpact", "stackimpact-go", "internal")

func isAgentStack(sample *profile.Sample) bool {
	return stackContains(sample, "", agentPath)
}

func stackContains(sample *profile.Sample, funcNameTest string, fileNameTest string) bool {
	for i := len(sample.Location) - 1; i >= 0; i-- {
		l := sample.Location[i]
		funcName, fileName, _ := readFuncInfo(l)

		if (funcNameTest == "" || strings.Contains(funcName, funcNameTest)) &&
			(fileNameTest == "" || strings.Contains(fileName, fileNameTest)) {
			return true
		}
	}

	return false
}

func readFuncInfo(l *profile.Location) (funcName string, fileName string, fileLine int64) {
	for li := range l.Line {
		if fn := l.Line[li].Function; fn != nil {
			return fn.Name, fn.Filename, l.Line[li].Line
		}
	}

	return "", "", 0
}
