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

const AgentVersion = "2.0.1"
const SAASDashboardAddress = "https://agent-api.stackimpact.com"

var agentStarted bool = false

type Agent struct {
	nextId  int64
	buildId string
	runId   string
	runTs   int64

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

	profilerLock *sync.Mutex

	// Options
	DashboardAddress string
	ProxyAddress     string
	AgentKey         string
	AppName          string
	AppVersion       string
	AppEnvironment   string
	HostName         string
	Debug            bool
	ProfileAgent     bool
}

func NewAgent() *Agent {
	a := &Agent{
		nextId:  0,
		runId:   "",
		buildId: "",
		runTs:   time.Now().Unix(),

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

		profilerLock: &sync.Mutex{},

		DashboardAddress: SAASDashboardAddress,
		ProxyAddress:     "",
		AgentKey:         "",
		AppName:          "",
		AppVersion:       "",
		AppEnvironment:   "",
		HostName:         "",
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
	a.processReporter.start()
	a.cpuReporter.start()
	a.allocationReporter.start()
	a.blockReporter.start()
	a.segmentReporter.start()
	a.errorReporter.start()

	a.log("Agent started.")

	return
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
			strconv.Itoa(rand.Intn(1000000000)) +
			strconv.FormatInt(n, 10)

	return sha1String(uuid)
}

func sha1String(s string) string {
	sha1 := sha1.New()
	sha1.Write([]byte(s))

	return hex.EncodeToString(sha1.Sum(nil))
}
