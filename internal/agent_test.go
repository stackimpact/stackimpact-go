package internal

import (
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStart(t *testing.T) {
	agent := NewAgent()
	agent.DashboardAddress = "http://localhost:5000"
	agent.AgentKey = "key"
	agent.AppName = "GoTestApp"
	agent.Debug = true
	agent.Start()

	if agent.AgentKey == "" {
		t.Error("AgentKey not set")
	}

	if agent.AppName == "" {
		t.Error("AppName not set")
	}

	if agent.HostName == "" {
		t.Error("HostName not set")
	}
}

func TestCalculateProgramSHA1(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true
	hash := agent.calculateProgramSHA1()

	if hash == "" {
		t.Error("failed calculating program SHA1")
	}
}

func TestStartStopProfiling(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true
	agent.AutoProfiling = false

	agent.cpuReporter.start()
	agent.StartProfiling("")

	time.Sleep(50 * time.Millisecond)

	agent.StopProfiling()

	if agent.cpuReporter.profileDuration == 0 {
		t.Error("profileDuration should be > 0")
	}
}

func TestManualCPUProfiler(t *testing.T) {
	payload := make(chan string)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zr, _ := gzip.NewReader(r.Body)
		body, _ := ioutil.ReadAll(zr)
		payload <- string(body)
		fmt.Fprintf(w, "{}")
	}))
	defer server.Close()

	agent := NewAgent()
	agent.Debug = true
	agent.AutoProfiling = false
	agent.ProfileAgent = true
	agent.DashboardAddress = server.URL

	go func() {
		agent.StartCPUProfiler()

		for i := 0; i < 10000000; i++ {
			rand.Intn(1000)
		}

		agent.StopCPUProfiler()
	}()

	payloadJson := <-payload
	if !strings.Contains(payloadJson, "TestManualCPUProfiler") {
		t.Error("The test function is not found in the payload")
	}
}

func TestManualBlockProfiler(t *testing.T) {
	payload := make(chan string)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zr, _ := gzip.NewReader(r.Body)
		body, _ := ioutil.ReadAll(zr)
		payload <- string(body)
		fmt.Fprintf(w, "{}")
	}))
	defer server.Close()

	agent := NewAgent()
	agent.Debug = true
	agent.AutoProfiling = false
	agent.ProfileAgent = true
	agent.DashboardAddress = server.URL

	go func() {
		agent.StartBlockProfiler()

		wait := make(chan bool)
		go func() {
			time.Sleep(150 * time.Millisecond)
			wait <- true
		}()
		<-wait

		agent.StopBlockProfiler()
	}()

	payloadJson := <-payload
	if !strings.Contains(payloadJson, "TestManualBlockProfiler") {
		t.Error("The test function is not found in the payload")
	}
}

func TestManualAllocationProfiler(t *testing.T) {
	payload := make(chan string)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zr, _ := gzip.NewReader(r.Body)
		body, _ := ioutil.ReadAll(zr)
		payload <- string(body)
		fmt.Fprintf(w, "{}")
	}))
	defer server.Close()

	agent := NewAgent()
	agent.Debug = true
	agent.AutoProfiling = false
	agent.ProfileAgent = true
	agent.DashboardAddress = server.URL

	go func() {
		objs = make([]string, 0)
		for i := 0; i < 100000; i++ {
			objs = append(objs, string(i))
		}

		agent.ReportAllocationProfile()
	}()

	payloadJson := <-payload
	if !strings.Contains(payloadJson, "TestManualAllocationProfiler") {
		t.Error("The test function is not found in the payload")
	}
}

func TestTimerPeriod(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	fired := 0
	timer := agent.createTimer(0, 10*time.Millisecond, func() {
		fired++
	})

	time.Sleep(20 * time.Millisecond)

	timer.Stop()

	time.Sleep(30 * time.Millisecond)

	if fired > 2 {
		t.Errorf("interval fired too many times: %v", fired)
	}
}

func TestTimerDelay(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	fired := 0
	timer := agent.createTimer(10*time.Millisecond, 0, func() {
		fired++
	})

	time.Sleep(20 * time.Millisecond)

	timer.Stop()

	if fired != 1 {
		t.Errorf("delay should fire once: %v", fired)
	}
}

func TestTimerDelayStop(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	fired := 0
	timer := agent.createTimer(10*time.Millisecond, 0, func() {
		fired++
	})

	timer.Stop()

	time.Sleep(20 * time.Millisecond)

	if fired == 1 {
		t.Errorf("delay should not fire")
	}
}
