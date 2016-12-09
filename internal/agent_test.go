package internal

import (
	"testing"
)

func TestStart(t *testing.T) {
	agent := NewAgent()
	agent.DashboardAddress = "http://localhost:5000"
	agent.AgentKey = "key"
	agent.AppName = "GoTestApp"
	agent.Debug = true
	agent.Start()

	if agent.AgentKey == "" {
		t.Error("AgentKey not set")
	}

	if agent.AppName == "" {
		t.Error("AppName not set")
	}

	if agent.HostName == "" {
		t.Error("HostName not set")
	}
}
