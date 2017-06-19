package internal

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

var verbose bool = false

func TestCreateBlockCallGraph(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true
	agent.ProfileAgent = true

	done := make(chan bool)

	go func() {
		time.Sleep(100 * time.Millisecond)

		wait := make(chan bool)

		go func() {
			time.Sleep(100 * time.Millisecond)

			wait <- true
		}()

		<-wait

		done <- true
	}()

	p, _ := agent.blockReporter.readBlockProfile(500)
	blockCallGraph, _, err := agent.blockReporter.createBlockCallGraph(p, 500)
	if err != nil {
		t.Error(err)
		return
	}

	if verbose {
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

func TestCreateHTTPCallGraph(t *testing.T) {
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

		if err := http.ListenAndServe(":5001", nil); err != nil {
			t.Error(err)
			return
		}
	}()

	waitForServer("http://localhost:5001/ready")

	go func() {
		time.Sleep(100 * time.Millisecond)

		res, err := http.Get("http://localhost:5001/test")
		if err != nil {
			t.Error(err)
		} else {
			defer res.Body.Close()
		}
	}()

	p, _ := agent.blockReporter.readBlockProfile(500)
	blockCallGraph, httpCallGraph, err := agent.blockReporter.createBlockCallGraph(p, 500)
	if err != nil {
		t.Error(err)
		return
	}

	httpCallGraph.convertToPercentage(blockCallGraph.measurement)

	if verbose {
		fmt.Printf("PERCENT: %v\n", httpCallGraph.measurement)
		fmt.Printf("CALL GRAPH: %v\n", httpCallGraph.printLevel(0))
	}
	if httpCallGraph.measurement < 5 {
		t.Errorf("HTTP percentage is too low: %v", httpCallGraph.measurement)
	}
	if httpCallGraph.numSamples < 1 {
		t.Error("Number of samples should be > 0")
	}

	if !strings.Contains(fmt.Sprintf("%v", httpCallGraph.toMap()), "TestCreateHTTPCallGraph") {
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
