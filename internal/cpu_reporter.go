package internal

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/stackimpact/stackimpact-go/internal/pprof/profile"
)

type CPUReporter struct {
	agent             *Agent
	profilerScheduler *ProfilerScheduler
	profile           *BreakdownNode
	profileDuration   int64
}

func newCPUReporter(agent *Agent) *CPUReporter {
	cr := &CPUReporter{
		agent:             agent,
		profilerScheduler: nil,
		profile:           nil,
		profileDuration:   0,
	}

	cr.profilerScheduler = newProfilerScheduler(agent, 10000, 2000, 120000,
		func(duration int64) {
			cr.record(duration)
		},
		func() {
			cr.report()
		},
	)

	return cr
}

func (cr *CPUReporter) start() {
	cr.reset()
	cr.profilerScheduler.start()
}

func (cr *CPUReporter) reset() {
	cr.profile = newBreakdownNode("root")
	cr.profileDuration = 0
}

func (cr *CPUReporter) record(duration int64) {
	if cr.agent.config.isProfilingDisabled() {
		return
	}

	cr.agent.log("Starting CPU profiler.")
	p, e := cr.readCPUProfile(duration)
	if e != nil {
		cr.agent.error(e)
		return
	}
	if p == nil {
		return
	}
	cr.agent.log("CPU profiler stopped.")

	if err := cr.updateCPUProfile(p); err != nil {
		cr.agent.error(err)
	}

	cr.profileDuration += duration
}

func (cr *CPUReporter) report() {
	if cr.agent.config.isProfilingDisabled() {
		return
	}

	cr.profile.convertToPercentage(float64(cr.profileDuration * 1e6 * int64(runtime.NumCPU())))

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

		cr.profile.increment(stackDuration, stackSamples)

		currentNode := cr.profile
		for i := len(s.Location) - 1; i >= 0; i-- {
			l := s.Location[i]
			funcName, fileName, fileLine := readFuncInfo(l)

			if funcName == "runtime.goexit" {
				continue
			}

			frameName := fmt.Sprintf("%v (%v:%v)", funcName, fileName, fileLine)
			currentNode = currentNode.findOrAddChild(frameName)
			currentNode.increment(stackDuration, stackSamples)
		}
	}

	return nil
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
