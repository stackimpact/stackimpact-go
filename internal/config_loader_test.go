package internal

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfigLoad(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "{\"profiling_disabled\":\"yes\"}")
	}))
	defer server.Close()

	agent := NewAgent()
	agent.AgentKey = "key1"
	agent.AppName = "App1"
	agent.HostName = "Host1"
	agent.Debug = true
	agent.DashboardAddress = server.URL

	agent.configLoader.load()

	if !agent.config.isProfilingDisabled() {
		t.Errorf("Config loading wasn't successful")
	}
}
