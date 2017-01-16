package internal

import (
	"sync"
)

type Config struct {
	agent             *Agent
	configLock        *sync.RWMutex
	profilingDisabled bool
}

func newConfig(agent *Agent) *Config {
	c := &Config{
		agent:             agent,
		configLock:        &sync.RWMutex{},
		profilingDisabled: false,
	}

	return c
}

func (c *Config) setProfilingDisabled(val bool) {
	c.configLock.Lock()
	defer c.configLock.Unlock()

	c.profilingDisabled = val
}

func (c *Config) isProfilingDisabled() bool {
	c.configLock.RLock()
	defer c.configLock.RUnlock()

	return c.profilingDisabled
}
