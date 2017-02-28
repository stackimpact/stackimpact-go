package stackimpact

import (
	"time"
)

type Segment struct {
	agent     *Agent
	Name      string
	timestamp float64
	Duration  float64
}

func newSegment(agent *Agent, name string) *Segment {
	s := &Segment{
		agent:    agent,
		Name:     name,
		Duration: 0,
	}

	return s
}

func (s *Segment) start() {
	s.timestamp = float64(time.Now().UnixNano()) / 1e6
}

// Stops the measurement of a code segment execution time.
func (s *Segment) Stop() {
	s.Duration = float64(time.Now().UnixNano())/1e6 - s.timestamp

	s.agent.internalAgent.RecordSegment(s.Name, s.Duration)
}
