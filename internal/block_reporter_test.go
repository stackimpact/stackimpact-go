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
		time.Sleep(100 * time.Millisecond)

		wait := make(chan bool)

		go func() {
			time.Sleep(100 * time.Millisecond)

			wait <- true
		}()

		<-wait

		done <- true
	}()

	agent.blockReporter.reset()
	p, _ := agent.blockReporter.readBlockProfile(500)
	err := agent.blockReporter.updateBlockProfile(p, 500)
	if err != nil {
		t.Error(err)
		return
	}

	blockCallGraph := agent.blockReporter.blockProfile
	blockCallGraph.normalize(0.5)

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

	agent.blockReporter.reset()
	p, _ := agent.blockReporter.readBlockProfile(500)
	err := agent.blockReporter.updateBlockProfile(p, 500)
	if err != nil {
		t.Error(err)
		return
	}

	blockCallGraph := agent.blockReporter.blockProfile
	blockCallGraph.normalize(0.5)

	httpCallGraph := agent.blockReporter.httpProfile

	httpCallGraph.normalize(0.5)
	httpCallGraph.convertToPercentage(blockCallGraph.measurement)

	if false {
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
