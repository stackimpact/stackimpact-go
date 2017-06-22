package internal

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"runtime"
	"runtime/pprof"
	"time"

	profile "github.com/stackimpact/stackimpact-go/internal/pprof/profile"
)

type BlockValues struct {
	delay       float64
	contentions int64
}

type BlockReporter struct {
	agent             *Agent
	profilerScheduler *ProfilerScheduler
	prevValues        map[string]*BlockValues
	blockProfile      *BreakdownNode
	httpProfile       *BreakdownNode
	profileDuration   int64
}

func newBlockReporter(agent *Agent) *BlockReporter {
	br := &BlockReporter{
		agent:             agent,
		profilerScheduler: nil,
		prevValues:        make(map[string]*BlockValues),
		blockProfile:      nil,
		httpProfile:       nil,
		profileDuration:   0,
	}

	br.profilerScheduler = newProfilerScheduler(agent, 10000, 2000, 120000,
		func(duration int64) {
			br.record(duration)
		},
		func() {
			br.report()
		},
	)

	return br
}

func (br *BlockReporter) start() {
	br.reset()
	br.profilerScheduler.start()
}

func (br *BlockReporter) reset() {
	br.blockProfile = newBreakdownNode("root")
	br.httpProfile = newBreakdownNode("root")
	br.profileDuration = 0
}

func (br *BlockReporter) record(duration int64) {
	if br.agent.config.isProfilingDisabled() {
		return
	}

	br.agent.log("Starting block profiler.")
	p, e := br.readBlockProfile(duration)
	if e != nil {
		br.agent.error(e)
		return
	}
	if p == nil {
		return
	}
	br.agent.log("Block profiler stopped.")

	err := br.updateBlockProfile(p, duration)
	if err != nil {
		br.agent.error(err)
		return
	}

	br.profileDuration += duration
}

func (br *BlockReporter) report() {
	durationSec := float64(br.profileDuration) / 1000

	br.blockProfile.normalize(durationSec)
	br.blockProfile.filter(2, 1, math.Inf(0))

	metric := newMetric(br.agent, TypeProfile, CategoryBlockProfile, NameBlockingCallTimes, UnitMillisecond)
	metric.createMeasurement(TriggerTimer, br.blockProfile.measurement, 1, br.blockProfile)
	br.agent.messageQueue.addMessage("metric", metric.toMap())

	if br.blockProfile.measurement > 0 && br.httpProfile.numSamples > 0 {
		br.httpProfile.normalize(durationSec)
		br.httpProfile.convertToPercentage(br.blockProfile.measurement)
		br.httpProfile.filter(2, 1, 100)

		metric := newMetric(br.agent, TypeProfile, CategoryHTTPTrace, NameHTTPTransactionBreakdown, UnitPercent)
		metric.createMeasurement(TriggerTimer, br.httpProfile.measurement, 0, br.httpProfile)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}

	br.reset()
}

func (br *BlockReporter) updateBlockProfile(p *profile.Profile, duration int64) error {
	contentionIndex := -1
	delayIndex := -1
	for i, s := range p.SampleType {
		if s.Type == "contentions" {
			contentionIndex = i
		} else if s.Type == "delay" {
			delayIndex = i
		}
	}

	if contentionIndex == -1 || delayIndex == -1 {
		return errors.New("Unrecognized profile data")
	}

	for _, s := range p.Sample {
		if !br.agent.ProfileAgent && isAgentStack(s) {
			continue
		}

		isHTTPStack := stackContains(s, ".ServeHTTP", "server.go")

		delay := float64(s.Value[delayIndex])
		contentions := s.Value[contentionIndex]

		valueKey := generateValueKey(s)
		delay, contentions = br.getValueChange(valueKey, delay, contentions)

		if contentions == 0 || delay == 0 {
			continue
		}

		// to milliseconds
		delay = delay / 1e6

		br.blockProfile.increment(delay, contentions)

		currentNode := br.blockProfile
		for i := len(s.Location) - 1; i >= 0; i-- {
			l := s.Location[i]
			funcName, fileName, fileLine := readFuncInfo(l)

			if funcName == "runtime.goexit" {
				continue
			}

			frameName := fmt.Sprintf("%v (%v:%v)", funcName, fileName, fileLine)
			currentNode = currentNode.findOrAddChild(frameName)
			currentNode.increment(delay, contentions)
		}

		if isHTTPStack {
			br.httpProfile.increment(delay, contentions)

			currentNode := br.httpProfile
			for i := len(s.Location) - 1; i >= 0; i-- {
				l := s.Location[i]
				funcName, fileName, fileLine := readFuncInfo(l)

				if funcName == "runtime.goexit" {
					continue
				}

				frameName := fmt.Sprintf("%v (%v:%v)", funcName, fileName, fileLine)
				currentNode = currentNode.findOrAddChild(frameName)
				currentNode.increment(delay, contentions)
			}
		}
	}

	return nil
}

func generateValueKey(s *profile.Sample) string {
	key := ""
	for _, l := range s.Location {
		key += fmt.Sprintf("%v:", l.Address)
	}

	return key
}

func (br *BlockReporter) getValueChange(key string, delay float64, contentions int64) (float64, int64) {
	if pv, exists := br.prevValues[key]; exists {
		delayChange := delay - pv.delay
		contentionsChange := contentions - pv.contentions

		pv.delay = delay
		pv.contentions = contentions

		return delayChange, contentionsChange
	} else {
		br.prevValues[key] = &BlockValues{
			delay:       delay,
			contentions: contentions,
		}

		return delay, contentions
	}
}

func (br *BlockReporter) readBlockProfile(duration int64) (*profile.Profile, error) {
	prof := pprof.Lookup("block")
	if prof == nil {
		return nil, errors.New("No block profile found")
	}

	runtime.SetBlockProfileRate(1e6)

	done := make(chan bool)
	timer := time.NewTimer(time.Duration(duration) * time.Millisecond)
	go func() {
		defer br.agent.recoverAndLog()

		<-timer.C

		runtime.SetBlockProfileRate(0)

		done <- true
	}()
	<-done

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	err := prof.WriteTo(w, 0)
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
