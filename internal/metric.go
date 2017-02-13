package internal

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"
)

const TypeState string = "state"
const TypeCounter string = "counter"
const TypeProfile string = "profile"
const TypeTrace string = "trace"

const CategoryCPU string = "cpu"
const CategoryMemory string = "memory"
const CategoryGC string = "gc"
const CategoryRuntime string = "runtime"
const CategorySegments string = "segments"
const CategoryCPUProfile string = "cpu-profile"
const CategoryMemoryProfile string = "memory-profile"
const CategoryChannelProfile string = "channel-profile"
const CategoryNetworkProfile string = "network-profile"
const CategorySystemProfile string = "system-profile"
const CategoryLockProfile string = "lock-profile"
const CategoryHTTPHandlerTrace string = "http-trace"
const CategoryHTTPClientTrace string = "http-client-trace"
const CategoryDBClientTrace string = "db-client-trace"
const CategorySegmentTrace string = "segment-trace"
const CategoryErrorProfile string = "error-profile"

const NameCPUTime string = "CPU time"
const NameCPUUsage string = "CPU usage"
const NameMaxRSS string = "Max RSS"
const NameCurrentRSS string = "Current RSS"
const NameVMSize string = "VM Size"
const NameNumGoroutines string = "Number of goroutines"
const NameNumCgoCalls string = "Number of cgo calls"
const NameAllocated string = "Allocated memory"
const NameLookups string = "Lookups"
const NameMallocs string = "Mallocs"
const NameFrees string = "Frees"
const NameHeapSys string = "Heap obtained"
const NameHeapIdle string = "Heap idle"
const NameHeapInuse string = "Heap non-idle"
const NameHeapReleased string = "Heap released"
const NameHeapObjects string = "Heap objects"
const NameGCTotalPause string = "GC total pause"
const NameNumGC string = "Number of GCs"
const NameGCCPUFraction string = "GC CPU fraction"
const NameHeapAllocation string = "Heap allocation"
const NameChannelWaitTime string = "Channel wait time"
const NameNetworkWaitTime string = "Network wait time"
const NameSystemWaitTime string = "System wait time"
const NameLockWaitTime string = "Lock wait time"
const NameHTTPTransactions string = "HTTP Transactions"
const NameHTTPCalls string = "HTTP Calls"
const NameSQLStatements string = "SQL Statements"
const NameMongoDBCalls string = "MongoDB Calls"
const NameRedisCalls string = "Redis Calls"

const UnitNone string = ""
const UnitMillisecond string = "millisecond"
const UnitMicrosecond string = "microsecond"
const UnitNanosecond string = "nanosecond"
const UnitByte string = "byte"
const UnitKilobyte string = "kilobyte"
const UnitPercent string = "percent"

const TriggerTimer string = "timer"
const TriggerAnomaly string = "anomaly"

const ReservoirSize int = 1000

type BreakdownNode struct {
	name        string
	measurement float64
	numSamples  int64
	reservoir   []float64
	children    map[string]*BreakdownNode
}

func newBreakdownNode(name string) *BreakdownNode {
	bn := &BreakdownNode{
		name:        name,
		measurement: 0,
		numSamples:  0,
		reservoir:   nil,
		children:    make(map[string]*BreakdownNode),
	}

	return bn
}

func (bn *BreakdownNode) findChild(name string) *BreakdownNode {
	if child, exists := bn.children[name]; exists {
		return child
	}

	return nil
}

func (bn *BreakdownNode) maxChild() *BreakdownNode {
	var maxChild *BreakdownNode = nil
	for _, child := range bn.children {
		if maxChild == nil || child.measurement > maxChild.measurement {
			maxChild = child
		}
	}
	return maxChild
}

func (bn *BreakdownNode) minChild() *BreakdownNode {
	var minChild *BreakdownNode = nil
	for _, child := range bn.children {
		if minChild == nil || child.measurement < minChild.measurement {
			minChild = child
		}
	}
	return minChild
}

func (bn *BreakdownNode) addChild(child *BreakdownNode) {
	bn.children[child.name] = child
}

func (bn *BreakdownNode) removeChild(child *BreakdownNode) {
	delete(bn.children, child.name)
}

func (bn *BreakdownNode) findOrAddChild(name string) *BreakdownNode {
	child := bn.findChild(name)
	if child == nil {
		child = newBreakdownNode(name)
		bn.addChild(child)
	}

	return child
}

func (bn *BreakdownNode) filter(fromLevel int, min float64, max float64) {
	bn.filterLevel(1, fromLevel, min, max)
}

func (bn *BreakdownNode) filterLevel(currentLevel int, fromLevel int, min float64, max float64) {
	for key, child := range bn.children {
		if currentLevel >= fromLevel && (child.measurement < min || child.measurement > max) {
			delete(bn.children, key)
		} else {
			child.filterLevel(currentLevel+1, fromLevel, min, max)
		}
	}
}

func (bn *BreakdownNode) depth() int {
	max := 0
	for _, child := range bn.children {
		cd := child.depth()
		if cd > max {
			max = cd
		}
	}

	return max + 1
}

func (bn *BreakdownNode) propagate() {
	for _, child := range bn.children {
		child.propagate()
		bn.measurement += child.measurement
		bn.numSamples += child.numSamples
	}
}

func (bn *BreakdownNode) increment(value float64, count int64) {
	bn.measurement += value
	bn.numSamples += count
}

func (bn *BreakdownNode) updateP95(value float64) {
	if bn.reservoir == nil {
		bn.reservoir = make([]float64, 0, ReservoirSize)
	}

	if len(bn.reservoir) < ReservoirSize {
		bn.reservoir = append(bn.reservoir, value)
	} else {
		bn.reservoir[rand.Intn(ReservoirSize)] = value
	}

	bn.numSamples += 1
}

func (bn *BreakdownNode) evaluateP95() {
	if bn.reservoir != nil && len(bn.reservoir) > 0 {
		sort.Float64s(bn.reservoir)
		index := int(math.Floor(float64(len(bn.reservoir)) / 100.0 * 95.0))
		bn.measurement = bn.reservoir[index]

		bn.reservoir = bn.reservoir[:0]
	}

	for _, child := range bn.children {
		child.evaluateP95()
	}
}

func (bn *BreakdownNode) toMap() map[string]interface{} {
	childrenMap := make([]interface{}, 0)
	for _, child := range bn.children {
		childrenMap = append(childrenMap, child.toMap())
	}

	nodeMap := map[string]interface{}{
		"name":        bn.name,
		"measurement": bn.measurement,
		"num_samples": bn.numSamples,
		"children":    childrenMap,
	}

	return nodeMap
}

func (bn *BreakdownNode) printLevel(level int) string {
	str := ""

	for i := 0; i < level; i++ {
		str += "  "
	}

	str += fmt.Sprintf("%v - %v\n", bn.name, bn.measurement)
	for _, child := range bn.children {
		str += child.printLevel(level + 1)
	}

	return str
}

type Measurement struct {
	id        string
	trigger   string
	value     float64
	breakdown *BreakdownNode
	timestamp int64
}

type Metric struct {
	agent        *Agent
	id           string
	typ          string
	category     string
	name         string
	unit         string
	measurement  *Measurement
	hasLastValue bool
	lastValue    float64
}

func newMetric(agent *Agent, typ string, category string, name string, unit string) *Metric {
	metricID := sha1String(agent.AppName + agent.AppEnvironment + agent.HostName + typ + category + name + unit)

	m := &Metric{
		agent:        agent,
		id:           metricID,
		typ:          typ,
		category:     category,
		name:         name,
		unit:         unit,
		measurement:  nil,
		hasLastValue: false,
		lastValue:    0,
	}

	return m
}

func (m *Metric) hasMeasurement() bool {
	return m.measurement != nil
}

func (m *Metric) createMeasurement(trigger string, value float64, breakdown *BreakdownNode) {
	ready := true

	if m.typ == TypeCounter {
		if !m.hasLastValue {
			ready = false
			m.hasLastValue = true
			m.lastValue = value
		} else {
			tmpValue := value
			value = value - m.lastValue
			m.lastValue = tmpValue
		}
	}

	if ready {
		m.measurement = &Measurement{
			id:        m.agent.uuid(),
			trigger:   trigger,
			value:     value,
			breakdown: breakdown,
			timestamp: time.Now().Unix(),
		}
	}
}

func (m *Metric) toMap() map[string]interface{} {
	var measurementMap map[string]interface{} = nil
	if m.measurement != nil {
		var breakdownMap map[string]interface{} = nil
		if m.measurement.breakdown != nil {
			breakdownMap = m.measurement.breakdown.toMap()
		}

		measurementMap = map[string]interface{}{
			"id":        m.measurement.id,
			"trigger":   m.measurement.trigger,
			"value":     m.measurement.value,
			"breakdown": breakdownMap,
			"timestamp": m.measurement.timestamp,
		}
	}

	metricMap := map[string]interface{}{
		"id":          m.id,
		"type":        m.typ,
		"category":    m.category,
		"name":        m.name,
		"unit":        m.unit,
		"measurement": measurementMap,
	}

	return metricMap
}
