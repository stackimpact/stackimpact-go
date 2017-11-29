package stackimpact

import (
	"sync/atomic"
)

type Span struct {
	agent   *Agent
	label   string
	started bool
	active  bool
}

func newSpan(agent *Agent, label string) *Span {
	s := &Span{
		agent:   agent,
		label:   label,
		started: false,
		active:  false,
	}

	return s
}

func (s *Span) start() {
	s.started = atomic.CompareAndSwapInt32(&s.agent.spanStarted, 0, 1)
	if s.started {
		s.active = s.agent.internalAgent.StartProfiling(s.label)
	}
}

// Stops profiling.
func (s *Span) Stop() {
	if s.started {
		if s.active {
			s.agent.internalAgent.StopProfiling()
		}
		atomic.StoreInt32(&s.agent.spanStarted, 0)
	}
}
