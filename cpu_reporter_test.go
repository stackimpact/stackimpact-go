package stackimpact

import (
	"fmt"
	"testing"
	//"time"
	"strconv"
	"strings"
)

func TestCreateCallGraph(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	done := make(chan bool)

	go func() {
		// cpu
		//start := time.Now().UnixNano()
		for i := 0; i < 5000000; i++ {
			str := "str" + strconv.Itoa(i)
			str = str + "a"
		}
		//took := time.Now().UnixNano() - start
		//fmt.Printf("TOOK: %v\n", took)

		done <- true
	}()

	p := agent.cpuReporter.readCPUProfile(1000)
	//fmt.Printf("PROFILE: %v\n", p.String())
	callGraph, err := agent.cpuReporter.createCPUCallGraph(p)
	if err != nil {
		t.Error(err)
		return
	}
	//fmt.Printf("CPU USAGE: %v\n", callGraph.measurement)
	//fmt.Printf("CALL GRAPH: %v\n", callGraph.printLevel(0))
	if callGraph.measurement < 50 {
		t.Error("CPU usage is too low")
	}

	if !strings.Contains(fmt.Sprintf("%v", callGraph.toMap()), "TestCreateCallGraph") {
		t.Error("The test function is not found in the profile")
	}

	<-done
}
