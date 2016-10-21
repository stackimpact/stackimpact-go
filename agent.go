package stackimpact

import (
	"github.com/stackimpact/stackimpact-go/internal"
)

type Agent struct {
	internalAgent    *internal.Agent
	DashboardAddress string
	HostName         string
	Debug            bool
}

func NewAgent() *Agent {
	a := &Agent{
		internalAgent:    internal.NewAgent(),
		DashboardAddress: "",
		HostName:         "",
		Debug:            false,
	}

	return a
}

func (a *Agent) Configure(agentKey string, appName string) {
	if a.DashboardAddress != "" {
		a.internalAgent.DashboardAddress = a.DashboardAddress
	}
	if a.HostName != "" {
		a.internalAgent.HostName = a.HostName
	}
	if a.Debug {
		a.internalAgent.Debug = a.Debug
	}

	a.internalAgent.Configure(agentKey, appName)
}
