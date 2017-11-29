package internal

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/stackimpact/stackimpact-go/internal/pprof/profile"
)

type CPUProfiler struct {
	agent      *Agent
	profile    *BreakdownNode
	profWriter *bufio.Writer
	profBuffer *bytes.Buffer
	startNano  int64
}

func newCPUProfiler(agent *Agent) *CPUProfiler {
	cp := &CPUProfiler{
		agent:      agent,
		profile:    nil,
		profWriter: nil,
		profBuffer: nil,
		startNano:  0,
	}

	return cp
}

func (cp *CPUProfiler) reset() {
	cp.profile = newBreakdownNode("root")
}

func (cp *CPUProfiler) startProfiler() error {
	err := cp.startCPUProfiler()
	if err != nil {
		return err
	}

	return nil
}

func (cp *CPUProfiler) stopProfiler() error {
	p, err := cp.stopCPUProfiler()
	if err != nil {
		return err
	}
	if p == nil {
		return errors.New("no profile returned")
	}

	if uerr := cp.updateCPUProfile(p); uerr != nil {
		return uerr
	}

	return nil
}

func (cp *CPUProfiler) buildProfile(duration int64) ([]*ProfileData, error) {
	cp.profile.convertToPercentage(float64(duration * int64(runtime.NumCPU())))
	cp.profile.propagate()
	// filter calls with lower than 1% CPU stake
	cp.profile.filter(2, 1, 100)

	data := []*ProfileData{
		&ProfileData{
			category:     CategoryCPUProfile,
			name:         NameCPUUsage,
			unit:         UnitPercent,
			unitInterval: 0,
			profile:      cp.profile,
		},
	}

	return data, nil
}

func (cp *CPUProfiler) updateCPUProfile(p *profile.Profile) error {
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
		if !cp.agent.ProfileAgent && isAgentStack(s) {
			continue
		}

		stackSamples := s.Value[samplesIndex]
		stackDuration := float64(s.Value[cpuIndex])

		currentNode := cp.profile
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

func (cp *CPUProfiler) startCPUProfiler() error {
	cp.profBuffer = &bytes.Buffer{}
	cp.profWriter = bufio.NewWriter(cp.profBuffer)
	cp.startNano = time.Now().UnixNano()

	err := pprof.StartCPUProfile(cp.profWriter)
	if err != nil {
		return err
	}

	return nil
}

func (cp *CPUProfiler) stopCPUProfiler() (*profile.Profile, error) {
	pprof.StopCPUProfile()

	cp.profWriter.Flush()
	r := bufio.NewReader(cp.profBuffer)

	if p, perr := profile.Parse(r); perr == nil {
		cp.profWriter = nil
		cp.profBuffer = nil

		if p.TimeNanos == 0 {
			p.TimeNanos = cp.startNano
		}
		if p.DurationNanos == 0 {
			p.DurationNanos = time.Now().UnixNano() - cp.startNano
		}

		if serr := symbolizeProfile(p); serr != nil {
			return nil, serr
		}

		if verr := p.CheckValid(); verr != nil {
			return nil, verr
		}

		return p, nil
	} else {
		cp.profWriter = nil
		cp.profBuffer = nil

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

func isAgentStack(sample *profile.Sample) bool {
	return stackContains(sample, "", agentPathInternal)
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
