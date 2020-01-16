package internal

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCreateBlockProfile(t *testing.T) {
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

	blockProfiler := newBlockProfiler(agent)
	blockProfiler.reset()
	blockProfiler.startProfiler()
	time.Sleep(500 * time.Millisecond)
	blockProfiler.stopProfiler()
	data, _ := blockProfiler.buildProfile(500*1e6, nil)
	blockProfile := data[0].profile

	if false {
		fmt.Printf("WAIT TIME: %v\n", blockProfile.measurement)
		fmt.Printf("CALL GRAPH: %v\n", blockProfile.printLevel(0))
	}
	if blockProfile.measurement < 100 {
		t.Errorf("Wait time is too low: %v", blockProfile.measurement)
	}
	if blockProfile.numSamples < 1 {
		t.Error("Number of samples should be > 0")
	}

	if !strings.Contains(fmt.Sprintf("%v", blockProfile.toMap()), "TestCreateBlockProfile") {
		t.Error("The test function is not found in the profile")
	}

	<-done
}

func waitForServer(url string) {
	for {
		if _, err := http.Get(url); err == nil {
			time.Sleep(100 * time.Millisecond)
			break
		}
	}
}
