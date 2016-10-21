package internal

import (
	"testing"
)

func TestCreateMeasurement(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	m := newMetric(agent, TypeCounter, CategoryCPU, NameCPUUsage, UnitNone)

	m.createMeasurement(TriggerTimer, 100, nil)

	if m.hasMeasurement() {
		t.Errorf("Should not have measurement")
	}

	m.createMeasurement(TriggerTimer, 110, nil)

	if m.measurement.value != 10 {
		t.Errorf("Value should be 10, but is %v", m.measurement.value)
	}

	m.createMeasurement(TriggerTimer, 115, nil)

	if m.measurement.value != 5 {
		t.Errorf("Value should be 5, but is %v", m.measurement.value)
	}

}

func TestBreakdownFilter(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	root := newBreakdownNode("root")
	root.measurement = 10

	child1 := newBreakdownNode("child1")
	child1.measurement = 9
	root.addChild(child1)

	child2 := newBreakdownNode("child2")
	child2.measurement = 1
	root.addChild(child2)

	child2child1 := newBreakdownNode("child2child1")
	child2child1.measurement = 1
	child2.addChild(child2child1)

	root.filter(3, 100)

	if root.findChild("child1") == nil {
		t.Errorf("child1 should not be filtered")
	}

	if root.findChild("child2") == nil {
		t.Errorf("child2 should not be filtered")
	}

	if child2.findChild("child2child1") != nil {
		t.Errorf("child2child1 should be filtered")
	}
}
