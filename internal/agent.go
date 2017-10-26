package internal

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const AgentVersion = "2.2.2"
const SAASDashboardAddress = "https://agent-api.stackimpact.com"

var agentStarted bool = false

type Agent struct {
	randSource *rand.Rand
	randLock   *sync.Mutex
	nextId     int64
	buildId    string
	runId      string
	runTs      int64

	apiRequest         *APIRequest
	config             *Config
	configLoader       *ConfigLoader
	messageQueue       *MessageQueue
	processReporter    *ProcessReporter
	cpuReporter        *CPUReporter
	allocationReporter *AllocationReporter
	blockReporter      *BlockReporter
	segmentReporter    *SegmentReporter
	errorReporter      *ErrorReporter

	profilerActive *Flag

	// Options
	DashboardAddress string
	ProxyAddress     string
	AgentKey         string
	AppName          string
	AppVersion       string
	AppEnvironment   string
	HostName         string
	AutoProfiling    bool
	Standalone       bool
	Debug            bool
	ProfileAgent     bool
}

func NewAgent() *Agent {
	a := &Agent{
		randSource: rand.New(rand.NewSource(time.Now().UnixNano())),
		randLock:   &sync.Mutex{},
		nextId:     0,
		runId:      "",
		buildId:    "",
		runTs:      time.Now().Unix(),

		apiRequest:         nil,
		config:             nil,
		configLoader:       nil,
		messageQueue:       nil,
		processReporter:    nil,
		cpuReporter:        nil,
		allocationReporter: nil,
		blockReporter:      nil,
		segmentReporter:    nil,
		errorReporter:      nil,

		profilerActive: &Flag{},

		DashboardAddress: SAASDashboardAddress,
		ProxyAddress:     "",
		AgentKey:         "",
		AppName:          "",
		AppVersion:       "",
		AppEnvironment:   "",
		HostName:         "",
		AutoProfiling:    true,
		Standalone:       false,
		Debug:            false,
		ProfileAgent:     false,
	}

	a.buildId = a.calculateProgramSHA1()
	a.runId = a.uuid()

	a.apiRequest = newAPIRequest(a)
	a.config = newConfig(a)
	a.configLoader = newConfigLoader(a)
	a.messageQueue = newMessageQueue(a)
	a.processReporter = newProcessReporter(a)
	a.cpuReporter = newCPUReporter(a)
	a.allocationReporter = newAllocationReporter(a)
	a.blockReporter = newBlockReporter(a)
	a.segmentReporter = newSegmentReporter(a)
	a.errorReporter = newErrorReporter(a)

	return a
}

func (a *Agent) Start() {
	if agentStarted {
		return
	}
	agentStarted = true

	if a.HostName == "" {
		hostName, err := os.Hostname()
		if err != nil {
			a.error(err)
		}
		a.HostName = hostName
	}

	a.configLoader.start()
	a.messageQueue.start()

	a.log("Agent started.")

	return
}

func (a *Agent) Enable() {
	if !a.config.isAgentEnabled() {
		a.cpuReporter.start()
		a.allocationReporter.start()
		a.blockReporter.start()
		a.segmentReporter.start()
		a.errorReporter.start()
		a.processReporter.start()
		a.config.setAgentEnabled(true)
	}
}

func (a *Agent) Disable() {
	if a.config.isAgentEnabled() {
		a.config.setAgentEnabled(false)
		a.cpuReporter.stop()
		a.allocationReporter.stop()
		a.blockReporter.stop()
		a.segmentReporter.stop()
		a.errorReporter.stop()
		a.processReporter.stop()
	}
}

func (a *Agent) StartProfiling() bool {
	defer a.recoverAndLog()

	if rand.Intn(2) == 0 {
		return a.cpuReporter.startProfiling(true) || a.blockReporter.startProfiling(true)
	} else {
		return a.blockReporter.startProfiling(true) || a.cpuReporter.startProfiling(true)
	}
}

func (a *Agent) StopProfiling() {
	defer a.recoverAndLog()

	a.cpuReporter.stopProfiling()
	a.blockReporter.stopProfiling()

	if !a.AutoProfiling {
		a.cpuReporter.report()
		a.allocationReporter.report()
		a.blockReporter.report()

		if !a.Standalone {
			a.messageQueue.flush()
			a.configLoader.load()
		}
	}
}

func (a *Agent) RecordSegment(name string, duration float64) {
	if !agentStarted {
		return
	}

	a.segmentReporter.recordSegment(name, duration)
}

func (a *Agent) RecordError(group string, msg interface{}, skipFrames int) {
	if !agentStarted {
		return
	}

	var err error
	switch v := msg.(type) {
	case error:
		err = v
	default:
		err = fmt.Errorf("%v", v)
	}

	a.errorReporter.recordError(group, err, skipFrames+1)
}

func (a *Agent) ReadMetrics() []interface{} {
	if !a.Standalone {
		return make([]interface{}, 0)
	}

	messages := a.messageQueue.read()

	metrics := make([]interface{}, 0)
	for _, message := range messages {
		if message.topic == "metric" {
			metrics = append(metrics, message.content)
		}
	}

	return metrics
}

func (a *Agent) log(format string, values ...interface{}) {
	if a.Debug {
		fmt.Printf("["+time.Now().Format(time.StampMilli)+"]"+
			" StackImpact "+AgentVersion+": "+
			format+"\n", values...)
	}
}

func (a *Agent) error(err error) {
	if a.Debug {
		fmt.Println("[" + time.Now().Format(time.StampMilli) + "]" +
			" StackImpact " + AgentVersion + ": Error")
		fmt.Println(err)
	}
}

func (a *Agent) recoverAndLog() {
	if err := recover(); err != nil {
		a.log("Recovered from panic in agent: %v", err)
	}
}

func (a *Agent) uuid() string {
	n := atomic.AddInt64(&a.nextId, 1)

	uuid :=
		strconv.FormatInt(time.Now().Unix(), 10) +
			strconv.FormatInt(a.random(1000000000), 10) +
			strconv.FormatInt(n, 10)

	return sha1String(uuid)
}

func (a *Agent) random(max int64) int64 {
	a.randLock.Lock()
	defer a.randLock.Unlock()

	return a.randSource.Int63n(max)
}

func sha1String(s string) string {
	sha1 := sha1.New()
	sha1.Write([]byte(s))

	return hex.EncodeToString(sha1.Sum(nil))
}

func (a *Agent) calculateProgramSHA1() string {
	file, err := os.Open(os.Args[0])
	if err != nil {
		a.error(err)
		return ""
	}
	defer file.Close()

	hash := sha1.New()
	if _, err := io.Copy(hash, file); err != nil {
		a.error(err)
		return ""
	}

	return hex.EncodeToString(hash.Sum(nil))
}

type Timer struct {
	agent              *Agent
	delayTimer         *time.Timer
	delayTimerDone     chan bool
	intervalTicker     *time.Ticker
	intervalTickerDone chan bool
	stopped            bool
}

func NewTimer(agent *Agent, delay time.Duration, interval time.Duration, job func()) *Timer {
	t := &Timer{
		agent:   agent,
		stopped: false,
	}

	t.delayTimerDone = make(chan bool)
	t.delayTimer = time.NewTimer(delay)
	go func() {
		defer t.agent.recoverAndLog()

		select {
		case <-t.delayTimer.C:
			if interval > 0 {
				t.intervalTickerDone = make(chan bool)
				t.intervalTicker = time.NewTicker(interval)
				go func() {
					defer t.agent.recoverAndLog()

					for {
						select {
						case <-t.intervalTicker.C:
							job()
						case <-t.intervalTickerDone:
							return
						}
					}
				}()
			}

			if delay > 0 {
				job()
			}
		case <-t.delayTimerDone:
			return
		}
	}()

	return t
}

func (t *Timer) Stop() {
	if !t.stopped {
		t.stopped = true

		t.delayTimer.Stop()
		close(t.delayTimerDone)

		if t.intervalTicker != nil {
			t.intervalTicker.Stop()
			close(t.intervalTickerDone)
		}
	}
}

func (a *Agent) createTimer(delay time.Duration, interval time.Duration, job func()) *Timer {
	return NewTimer(a, delay, interval, job)
}

type Flag struct {
	value int32
}

func (f *Flag) SetIfUnset() bool {
	return atomic.CompareAndSwapInt32(&f.value, 0, 1)
}

func (f *Flag) UnsetIfSet() bool {
	return atomic.CompareAndSwapInt32(&f.value, 1, 0)
}

func (f *Flag) Set() {
	atomic.StoreInt32(&f.value, 1)
}

func (f *Flag) Unset() {
	atomic.StoreInt32(&f.value, 0)
}

func (f *Flag) IsSet() bool {
	return atomic.LoadInt32(&f.value) == 1
}
