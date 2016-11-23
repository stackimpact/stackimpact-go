package stackimpact

import (
	"time"
)

type Segment struct {
	agent     *Agent
	Path      []string
	timestamp int64
	Duration  int64
}

func newSegment(agent *Agent, path []string) *Segment {
	s := &Segment{
		agent:    agent,
		Path:     path,
		Duration: 0,
	}

	return s
}

func (s *Segment) start() {
	s.timestamp = time.Now().UnixNano() / 1e6
}

func (s *Segment) Stop() {
	s.Duration = time.Now().UnixNano()/1e6 - s.timestamp

	s.agent.internalAgent.RecordSegment(s.Path, s.Duration)
}
