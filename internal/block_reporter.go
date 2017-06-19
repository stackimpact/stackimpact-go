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
	agent           *Agent
	profilerTrigger *ProfilerTrigger
	prevValues      map[string]*BlockValues
}

func newBlockReporter(agent *Agent) *BlockReporter {
	br := &BlockReporter{
		agent:           agent,
		profilerTrigger: nil,
		prevValues:      make(map[string]*BlockValues),
	}

	br.profilerTrigger = newProfilerTrigger(agent, 75, 300,
		func() map[string]float64 {
			metrics := map[string]float64{"num-goroutines": float64(runtime.NumGoroutine())}

			segmentDurations := br.agent.segmentReporter.readLastDurations()
			for name, duration := range segmentDurations {
				metrics[name] = duration
			}

			return metrics
		},
		func(trigger string) {
			br.agent.log("Trace report triggered by reporting strategy, trigger=%v", trigger)
			br.report(trigger)
		},
	)

	return br
}

func (br *BlockReporter) start() {
	br.profilerTrigger.start()
}

func (br *BlockReporter) report(trigger string) {
	if br.agent.config.isProfilingDisabled() {
		return
	}

	duration := int64(5000)

	br.agent.log("Starting block profiler for %v milliseconds...", duration)
	p, e := br.readBlockProfile(duration)
	if e != nil {
		br.agent.error(e)
		return
	}
	if p == nil {
		return
	}
	br.agent.log("block profiler stopped.")

	blockCallGraph, httpCallGraph, err := br.createBlockCallGraph(p, duration)
	if err != nil {
		br.agent.error(err)
		return
	}

	blockCallGraph.filter(2, 1, math.Inf(0))

	metric := newMetric(br.agent, TypeProfile, CategoryBlockProfile, NameBlockingCallTimes, UnitMillisecond)
	metric.createMeasurement(trigger, blockCallGraph.measurement, 1, blockCallGraph)
	br.agent.messageQueue.addMessage("metric", metric.toMap())

	if blockCallGraph.measurement > 0 && httpCallGraph.numSamples > 0 {
		httpCallGraph.convertToPercentage(blockCallGraph.measurement)
		httpCallGraph.filter(2, 1, 100)

		metric := newMetric(br.agent, TypeProfile, CategoryHTTPTrace, NameHTTPTransactionBreakdown, UnitPercent)
		metric.createMeasurement(trigger, httpCallGraph.measurement, 0, httpCallGraph)
		br.agent.messageQueue.addMessage("metric", metric.toMap())
	}
}

func (br *BlockReporter) createBlockCallGraph(p *profile.Profile, duration int64) (*BreakdownNode, *BreakdownNode, error) {
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
		return nil, nil, errors.New("Unrecognized profile data")
	}

	durationSec := float64(duration) / 1000.0

	// build call graphs
	blockRootNode := newBreakdownNode("root")
	httpRootNode := newBreakdownNode("root")

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

		// scale to milliseconds per second
		contentions = int64(math.Ceil(float64(contentions) / durationSec))
		delay = (delay / 1e6) / durationSec

		blockRootNode.increment(delay, contentions)

		currentNode := blockRootNode
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
			httpRootNode.increment(delay, contentions)

			currentNode := httpRootNode
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

	return blockRootNode, httpRootNode, nil
}

func generateValueKey(s *profile.Sample) string {
	key := ""
	for _, l := range s.Location {
		key += fmt.Sprintf("%v:", l.ID)
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
