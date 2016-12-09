package stackimpact

import (
	"github.com/stackimpact/stackimpact-go/internal"
)

const ErrorGroupRecoveredPanics string = "Recovered panics"
const ErrorGroupUnrecoveredPanics string = "Unrecovered panics"
const ErrorGroupHandledExceptions string = "Handled exceptions"

type Options struct {
	DashboardAddress string
	AgentKey         string
	AppName          string
	AppVersion       string
	AppEnvironment   string
	HostName         string
	Debug            bool
}

type Agent struct {
	internalAgent *internal.Agent

	// compatibility < 1.2.0
	DashboardAddress string
	AgentKey         string
	AppName          string
	HostName         string
	Debug            bool
}

func NewAgent() *Agent {
	a := &Agent{
		internalAgent: internal.NewAgent(),
	}

	return a
}

func (a *Agent) Start(options Options) {
	a.internalAgent.AgentKey = options.AgentKey
	a.internalAgent.AppName = options.AppName

	if options.AppVersion != "" {
		a.internalAgent.AppVersion = options.AppVersion
	}

	if options.AppEnvironment != "" {
		a.internalAgent.AppEnvironment = options.AppEnvironment
	}

	if options.HostName != "" {
		a.internalAgent.HostName = options.HostName
	}

	if options.DashboardAddress != "" {
		a.internalAgent.DashboardAddress = options.DashboardAddress
	}

	if options.Debug {
		a.internalAgent.Debug = options.Debug
	}

	a.internalAgent.Start()
}

// compatibility < 1.2.0
func (a *Agent) Configure(agentKey string, appName string) {
	a.Start(Options{
		AgentKey:         agentKey,
		AppName:          appName,
		HostName:         a.HostName,
		DashboardAddress: a.DashboardAddress,
		Debug:            a.Debug,
	})
}

func (a *Agent) MeasureSegment(segmentName string) *Segment {
	s := newSegment(a, []string{segmentName})
	s.start()

	return s
}

func (a *Agent) MeasureSubsegment(segmentName string, subsegmentName string) *Segment {
	s := newSegment(a, []string{segmentName, subsegmentName})
	s.start()

	return s
}

func (a *Agent) RecordError(err interface{}) {
	a.internalAgent.RecordError(ErrorGroupHandledExceptions, err, 1)
}

func (a *Agent) RecordPanic() {
	if err := recover(); err != nil {
		a.internalAgent.RecordError(ErrorGroupUnrecoveredPanics, err, 1)

		panic(err)
	}
}

func (a *Agent) RecordAndRecoverPanic() {
	if err := recover(); err != nil {
		a.internalAgent.RecordError(ErrorGroupRecoveredPanics, err, 1)
	}
}
