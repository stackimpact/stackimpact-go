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
		for {
			defer cl.agent.recoverAndLog()

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
		// profiling_enabled yes|no
		if profilingDisabled, exists := config["profiling_disabled"]; exists {
			cl.agent.config.setProfilingDisabled(profilingDisabled.(string) == "yes")
		} else {
			cl.agent.config.setProfilingDisabled(false)
		}
	} else {
		cl.agent.log("Error loading config from Dashboard")
		cl.agent.error(err)
	}
}
