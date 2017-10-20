package internal

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateBlockCallGraph(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true
	agent.ProfileAgent = true

	done := make(chan bool)

	go func() {
		time.Sleep(200 * time.Millisecond)

		wait := make(chan bool)

		go func() {
			time.Sleep(150 * time.Millisecond)

			wait <- true
		}()

		<-wait

		done <- true
	}()

	agent.blockReporter.started.Set()
	agent.blockReporter.reset()
	agent.blockReporter.startProfiling(false)
	time.Sleep(500 * time.Millisecond)
	agent.blockReporter.stopProfiling()

	blockCallGraph := agent.blockReporter.blockProfile
	blockCallGraph.normalize(0.5)
	blockCallGraph.propagate()

	if false {
		fmt.Printf("WAIT TIME: %v\n", blockCallGraph.measurement)
		fmt.Printf("CALL GRAPH: %v\n", blockCallGraph.printLevel(0))
	}
	if blockCallGraph.measurement < 100 {
		t.Errorf("Wait time is too low: %v", blockCallGraph.measurement)
	}
	if blockCallGraph.numSamples < 1 {
		t.Error("Number of samples should be > 0")
	}

	if !strings.Contains(fmt.Sprintf("%v", blockCallGraph.toMap()), "TestCreateBlockCallGraph") {
		t.Error("The test function is not found in the profile")
	}

	<-done
}

func TestCreateBlockTraceCallGraph(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true
	agent.ProfileAgent = true

	// start HTTP server
	go func() {
		http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "OK")
		})

		http.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
			lock := &sync.Mutex{}
			lock.Lock()

			go func() {
				time.Sleep(100 * time.Millisecond)
				lock.Unlock()
			}()

			lock.Lock()

			fmt.Fprintf(w, "OK")
		})

		if err := http.ListenAndServe(":6001", nil); err != nil {
			t.Error(err)
			return
		}
	}()

	waitForServer("http://localhost:6001/ready")

	go func() {
		time.Sleep(100 * time.Millisecond)

		res, err := http.Get("http://localhost:6001/test")
		if err != nil {
			t.Error(err)
		} else {
			defer res.Body.Close()
		}
	}()

	agent.blockReporter.started.Set()
	agent.blockReporter.reset()
	agent.blockReporter.startProfiling(false)
	time.Sleep(500 * time.Millisecond)
	agent.blockReporter.stopProfiling()

	blockTraceCallGraph := agent.blockReporter.blockTrace
	blockTraceCallGraph.evaluateP95()
	blockTraceCallGraph.propagate()
	blockTraceCallGraph.round()

	if false {
		fmt.Printf("LATENCY: %v\n", blockTraceCallGraph.measurement)
		fmt.Printf("CALL GRAPH: %v\n", blockTraceCallGraph.printLevel(0))
	}
	if blockTraceCallGraph.measurement < 5 {
		t.Errorf("Block trace value is too low: %v", blockTraceCallGraph.measurement)
	}
	if blockTraceCallGraph.numSamples < 1 {
		t.Error("Number of samples should be > 0")
	}

	if !strings.Contains(fmt.Sprintf("%v", blockTraceCallGraph.toMap()), "TestCreateBlockTraceCallGraph") {
		t.Error("The test function is not found in the profile")
	}
}

func waitForServer(url string) {
	for {
		if _, err := http.Get(url); err == nil {
			time.Sleep(100 * time.Millisecond)
			break
		}
	}
}
