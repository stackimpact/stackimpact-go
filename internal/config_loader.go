package internal

import (
	"time"
)

type ConfigLoader struct {
	agent *Agent
}

func newConfigLoader(agent *Agent) *ConfigLoader {
	cl := &ConfigLoader{
		agent: agent,
	}

	return cl
}

func (cl *ConfigLoader) start() {
	loadDelay := time.NewTimer(2 * time.Second)
	go func() {
		defer cl.agent.recoverAndLog()

		<-loadDelay.C
		cl.load()
	}()

	loadTicker := time.NewTicker(120 * time.Second)
	go func() {
		defer cl.agent.recoverAndLog()

		for {
			select {
			case <-loadTicker.C:
				cl.load()
			}
		}
	}()
}

func (cl *ConfigLoader) load() {
	payload := map[string]interface{}{}
	if config, err := cl.agent.apiRequest.post("config", payload); err == nil {
		// agent_enabled yes|no
		if agentEnabled, exists := config["agent_enabled"]; exists {
			cl.agent.config.setAgentEnabled(agentEnabled.(string) == "yes")
		} else {
			cl.agent.config.setAgentEnabled(false)
		}

		// profiling_enabled yes|no
		if profilingDisabled, exists := config["profiling_disabled"]; exists {
			cl.agent.config.setProfilingDisabled(profilingDisabled.(string) == "yes")
		} else {
			cl.agent.config.setProfilingDisabled(false)
		}

		if cl.agent.config.isAgentEnabled() && !cl.agent.config.isProfilingDisabled() {
			cl.agent.cpuReporter.start()
			cl.agent.allocationReporter.start()
			cl.agent.blockReporter.start()
		} else {
			cl.agent.cpuReporter.stop()
			cl.agent.allocationReporter.stop()
			cl.agent.blockReporter.stop()
		}

		if cl.agent.config.isAgentEnabled() {
			cl.agent.segmentReporter.start()
			cl.agent.errorReporter.start()
			cl.agent.processReporter.start()
		} else {
			cl.agent.segmentReporter.stop()
			cl.agent.errorReporter.stop()
			cl.agent.processReporter.stop()
		}
	} else {
		cl.agent.log("Error loading config from Dashboard")
		cl.agent.error(err)
	}
}
