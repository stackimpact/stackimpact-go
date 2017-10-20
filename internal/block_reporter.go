package internal

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"

	profile "github.com/stackimpact/stackimpact-go/internal/pprof/profile"
)

type BlockValues struct {
	delay       float64
	contentions int64
}

type BlockReporter struct {
	MaxProfileDuration int64
	MaxSpanDuration    int64
	MaxSpanCount       int32
	SpanInterval       int64
	ReportInterval     int64

	agent       *Agent
	started     *Flag
	spanTimer   *Timer
	reportTimer *Timer

	blockProfile          *BreakdownNode
	blockTrace            *BreakdownNode
	profileLock           *sync.Mutex
	profileStartTimestamp int64
	profileDuration       int64
	prevValues            map[string]*BlockValues
	spanCount             int32
	spanActive            *Flag
	spanProfile           *pprof.Profile
	spanStart             int64
	spanTimeout           *Timer
}

func newBlockReporter(agent *Agent) *BlockReporter {
	br := &BlockReporter{
		MaxProfileDuration: 20,
		MaxSpanDuration:    4,
		MaxSpanCount:       30,
		SpanInterval:       16,
		ReportInterval:     120,

		agent:       agent,
		started:     &Flag{},
		spanTimer:   nil,
		reportTimer: nil,

		blockProfile:          nil,
		blockTrace:            nil,
		profileLock:           &sync.Mutex{},
		profileStartTimestamp: 0,
		profileDuration:       0,
		prevValues:            make(map[string]*BlockValues),
		spanCount:             0,
		spanActive:            &Flag{},
		spanProfile:           nil,
		spanStart:             0,
		spanTimeout:           nil,
	}

	return br
}

func (br *BlockReporter) start() {
	if !br.started.SetIfUnset() {
		return
	}

	br.profileLock.Lock()
	defer br.profileLock.Unlock()

	br.reset()

	if br.agent.AutoProfiling {
		br.spanTimer = br.agent.createTimer(0, time.Duration(br.SpanInterval)*time.Second, func() {
			time.Sleep(time.Duration(rand.Int63n(br.SpanInterval-br.MaxSpanDuration)) * time.Second)
			br.startProfiling(false)
		})

		br.reportTimer = br.agent.createTimer(0, time.Duration(br.ReportInterval)*time.Second, func() {
			br.report()
		})
	}
}

func (br *BlockReporter) stop() {
	if !br.started.UnsetIfSet() {
		return
	}

	if br.spanTimer != nil {
		br.spanTimer.Stop()
	}

	if br.reportTimer != nil {
		br.reportTimer.Stop()
	}
}

func (br *BlockReporter) reset() {
	br.blockProfile = newBreakdownNode("root")
	br.blockTrace = newBreakdownNode("root")
	br.profileStartTimestamp = time.Now().Unix()
	br.profileDuration = 0
	br.spanCount = 0
}

func (br *BlockReporter) startProfiling(rateLimit bool) bool {
	if !br.started.IsSet() {
		return false
	}

	br.profileLock.Lock()
	defer br.profileLock.Unlock()

	if br.profileDuration > br.MaxProfileDuration*1e9 {
		br.agent.log("Block profiler: max profiling duration reached.")
		return false
	}

	if rateLimit && br.spanCount > br.MaxSpanCount {
		br.agent.log("Block profiler: max profiling span count reached.")
		return false
	}

	if !br.agent.profilerActive.SetIfUnset() {
		br.agent.log("Block profiler: another profiler currently active.")
		return false
	}

	br.agent.log("Starting Block profiler.")

	err := br.startBlockProfiler()
	if err != nil {
		br.agent.profilerActive.Unset()
		br.agent.error(err)
		return false
	}

	br.spanTimeout = br.agent.createTimer(time.Duration(br.MaxSpanDuration)*time.Second, 0, func() {
		br.stopProfiling()
	})

	br.spanCount++
	br.spanActive.Set()
	br.spanStart = time.Now().UnixNano()

	return true
}

func (br *BlockReporter) stopProfiling() {
	br.profileLock.Lock()
	defer br.profileLock.Unlock()

	if !br.spanActive.UnsetIfSet() {
		return
	}
	br.spanTimeout.Stop()

	defer br.agent.profilerActive.Unset()

	p, err := br.stopBlockProfiler()
	if err != nil {
		br.agent.error(err)
		return
	}
	if p == nil {
		return
	}
	br.agent.log("Block profiler stopped.")

	if uerr := br.updateBlockProfile(p); uerr != nil {
		br.agent.error(uerr)
	}

	br.profileDuration += time.Now().UnixNano() - br.spanStart
}

func (br *BlockReporter) report() {
	if !br.started.IsSet() {
		return
	}

	br.profileLock.Lock()
	defer br.profileLock.Unlock()

	if !br.agent.AutoProfiling && br.profileStartTimestamp > time.Now().Unix()-br.ReportInterval {
		return
	}

	if br.profileDuration == 0 {
		return
	}

	br.agent.log("Block profiler: reporting profile.")

	br.blockProfile.normalize(float64(br.profileDuration) / 1e9)
	br.blockProfile.propagate()
	br.blockProfile.filter(2, 1, math.Inf(0))

	metric := newMetric(br.agent, TypeProfile, CategoryBlockProfile, NameBlockingCallTimes, UnitMillisecond)
	metric.createMeasurement(TriggerTimer, br.blockProfile.measurement, 1, br.blockProfile)
	br.agent.messageQueue.addMessage("metric", metric.toMap())

	br.blockTrace.evaluateP95()
	br.blockTrace.propagate()
	br.blockTrace.round()
	br.blockTrace.filter(2, 1, math.Inf(0))

	metric = newMetric(br.agent, TypeProfile, CategoryBlockTrace, NameBlockingCallTimes, UnitMillisecond)
	metric.createMeasurement(TriggerTimer, br.blockTrace.measurement, 0, br.blockTrace)
	br.agent.messageQueue.addMessage("metric", metric.toMap())

	br.reset()
}

func (br *BlockReporter) updateBlockProfile(p *profile.Profile) error {
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

		delay := float64(s.Value[delayIndex])
		contentions := s.Value[contentionIndex]

		valueKey := generateValueKey(s)
		delay, contentions = br.getValueChange(valueKey, delay, contentions)

		if contentions == 0 || delay == 0 {
			continue
		}

		// to milliseconds
		delay = delay / 1e6

		currentNode := br.blockProfile
		for i := len(s.Location) - 1; i >= 0; i-- {
			l := s.Location[i]
			funcName, fileName, fileLine := readFuncInfo(l)

			if funcName == "runtime.goexit" {
				continue
			}

			frameName := fmt.Sprintf("%v (%v:%v)", funcName, fileName, fileLine)
			currentNode = currentNode.findOrAddChild(frameName)
		}
		currentNode.increment(delay, contentions)

		currentNode = br.blockTrace
		for i := len(s.Location) - 1; i >= 0; i-- {
			l := s.Location[i]
			funcName, fileName, fileLine := readFuncInfo(l)

			if funcName == "runtime.goexit" {
				continue
			}

			frameName := fmt.Sprintf("%v (%v:%v)", funcName, fileName, fileLine)
			currentNode = currentNode.findOrAddChild(frameName)
		}
		currentNode.updateP95(delay / float64(contentions))
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

func (br *BlockReporter) startBlockProfiler() error {
	br.spanProfile = pprof.Lookup("block")
	if br.spanProfile == nil {
		return errors.New("No block profile found")
	}

	runtime.SetBlockProfileRate(1e6)

	return nil
}

func (br *BlockReporter) stopBlockProfiler() (*profile.Profile, error) {
	runtime.SetBlockProfileRate(0)

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	err := br.spanProfile.WriteTo(w, 0)
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
