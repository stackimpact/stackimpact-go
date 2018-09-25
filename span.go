package stackimpact

import (
	"sync/atomic"
	"time"
)

type Span struct {
	agent     *Agent
	name      string
	timestamp time.Time
	started   bool
	active    bool
}

func newSpan(agent *Agent, name string) *Span {
	s := &Span{
		agent:   agent,
		name:    name,
		started: false,
		active:  false,
	}

	return s
}

func (s *Span) start() {
	s.started = atomic.CompareAndSwapInt32(&s.agent.spanStarted, 0, 1)
	if s.started {
		s.active = s.agent.internalAgent.StartProfiling(s.name)
	}

	s.timestamp = time.Now()
}

// Stops profiling.
func (s *Span) Stop() {
	duration := float64(time.Since(s.timestamp).Nanoseconds()) / 1e6
	s.agent.internalAgent.RecordSpan(s.name, duration)

	if s.started {
		if s.active {
			s.agent.internalAgent.StopProfiling()
		}

		if !s.agent.internalAgent.AutoProfiling {
			s.agent.internalAgent.Report()
		}

		atomic.StoreInt32(&s.agent.spanStarted, 0)
	}
}
