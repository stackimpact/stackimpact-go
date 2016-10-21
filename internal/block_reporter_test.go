package internal

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	pprofTrace "github.com/stackimpact/stackimpact-go/internal/pprof/trace"
)

func TestCreateBlockCallGraphWithChannel(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	done := make(chan bool)

	go func() {
		time.Sleep(10 * time.Millisecond)

		wait := make(chan bool)

		go func() {
			time.Sleep(500 * time.Millisecond)

			wait <- true
		}()

		<-wait

		done <- true
	}()

	events := agent.blockReporter.readTraceEvents(1000)

	/*fmt.Printf("EVENTS:\n")
	  for _, ev := range events {
	    for _, f := range ev.Stk {
	      fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
	    }
	    fmt.Printf("\n")
	  }*/

	selectedEvents := selectEventsByType(events, pprofTrace.EvGoBlockRecv)
	callGraph, err := agent.blockReporter.createBlockCallGraph(selectedEvents, nil, 1000)
	if err != nil {
		t.Error(err)
		return
	}
	//fmt.Printf("WAIT TIME: %v\n", callGraph.measurement)
	//fmt.Printf("CALL GRAPH: %v\n", callGraph.printLevel(0))
	if callGraph.measurement < 100 {
		t.Error("Wait time is too low")
	}

	if !strings.Contains(fmt.Sprintf("%v", callGraph.toMap()), "TestCreateBlockCallGraphWithChannel") {
		t.Error("The test function is not found in the profile")
	}

	<-done
}

func TestCreateBlockCallGraphWithNetwork(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	go func() {
		http.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(500 * time.Millisecond)
			fmt.Fprintf(w, "OK")
		})

		if err := http.ListenAndServe(":5000", nil); err != nil {
			t.Error(err)
			return
		}
	}()

	done := make(chan bool)

	go func() {
		time.Sleep(10 * time.Millisecond)

		res, err := http.Get("http://localhost:5000/test")
		if err != nil {
			t.Error(err)
		} else {
			defer res.Body.Close()
		}

		done <- true
	}()

	events := agent.blockReporter.readTraceEvents(1000)

	/*fmt.Printf("EVENTS:\n")
	  for _, ev := range events {
	    for _, f := range ev.Stk {
	      fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
	    }
	    fmt.Printf("\n")
	  }*/

	selectedEvents := selectEventsByType(events, pprofTrace.EvGoBlockNet)
	callGraph, err := agent.blockReporter.createBlockCallGraph(selectedEvents, nil, 1000)
	if err != nil {
		t.Error(err)
		return
	}
	//fmt.Printf("WAIT TIME: %v\n", callGraph.measurement)
	//fmt.Printf("CALL GRAPH: %v\n", callGraph.printLevel(0))
	if callGraph.measurement < 100 {
		t.Error("Wait time is too low")
	}

	if !strings.Contains(fmt.Sprintf("%v", callGraph.toMap()), "TestCreateBlockCallGraphWithNetwork") {
		t.Error("The test function is not found in the profile")
	}

	<-done
}

func TestCreateBlockCallGraphWithLock(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	done := make(chan bool)

	lock := &sync.Mutex{}
	lock.Lock()

	go func() {
		time.Sleep(10 * time.Millisecond)
		lock.Lock()

		done <- true
	}()

	go func() {
		time.Sleep(500 * time.Millisecond)
		lock.Unlock()
	}()

	events := agent.blockReporter.readTraceEvents(1000)

	/*fmt.Printf("EVENTS:\n")
	  for _, ev := range events {
	    for _, f := range ev.Stk {
	      fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
	    }
	    fmt.Printf("\n")
	  }*/

	selectedEvents := selectEventsByType(events, pprofTrace.EvGoBlockSync)
	callGraph, err := agent.blockReporter.createBlockCallGraph(selectedEvents, nil, 1000)
	if err != nil {
		t.Error(err)
		return
	}
	//fmt.Printf("WAIT TIME: %v\n", callGraph.measurement)
	//fmt.Printf("CALL GRAPH: %v\n", callGraph.printLevel(0))
	if callGraph.measurement < 100 {
		t.Error("Wait time is too low")
	}

	if !strings.Contains(fmt.Sprintf("%v", callGraph.toMap()), "TestCreateBlockCallGraphWithLock") {
		t.Error("The test function is not found in the profile")
	}

	<-done
}

func TestCreateBlockCallGraphWithSyscall(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	done := make(chan bool)

	go func() {
		time.Sleep(10 * time.Millisecond)
		_, err := exec.Command("sleep", "1").Output()
		if err != nil {
			t.Error(err)
		}

		done <- true
	}()

	events := agent.blockReporter.readTraceEvents(2000)

	/*fmt.Printf("EVENTS:\n")
	  for _, ev := range events {
	    for _, f := range ev.Stk {
	      fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
	    }
	    fmt.Printf("\n")
	  }*/

	selectedEvents := selectEventsByType(events, pprofTrace.EvGoSysCall)
	callGraph, err := agent.blockReporter.createBlockCallGraph(selectedEvents, nil, 2000)
	if err != nil {
		t.Error(err)
		return
	}
	//fmt.Printf("WAIT TIME: %v\n", callGraph.measurement)
	//fmt.Printf("CALL GRAPH: %v\n", callGraph.printLevel(0))
	if callGraph.measurement < 100 {
		t.Error("Wait time is too low")
	}

	if !strings.Contains(fmt.Sprintf("%v", callGraph.toMap()), "TestCreateBlockCallGraphWithSyscall") {
		t.Error("The test function is not found in the profile")
	}

	<-done
}

func TestCreateBlockCallGraphWithTrace(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	done := make(chan bool)

	// start HTTP server
	go func() {
		http.HandleFunc("/test2", func(w http.ResponseWriter, r *http.Request) {
			lock := &sync.Mutex{}
			lock.Lock()

			go func() {
				time.Sleep(100 * time.Millisecond)
				lock.Unlock()
			}()

			lock.Lock()

			fmt.Fprintf(w, "OK")
		})

		if err := http.ListenAndServe(":5002", nil); err != nil {
			t.Error(err)
			return
		}
	}()

	go func() {
		time.Sleep(10 * time.Millisecond)

		// request 1
		res, err := http.Get("http://localhost:5002/test2")
		if err != nil {
			t.Error(err)
		} else {
			defer res.Body.Close()
		}

		time.Sleep(10 * time.Millisecond)

		// request 2
		res, err = http.Get("http://localhost:5002/test2")
		if err != nil {
			t.Error(err)
		} else {
			defer res.Body.Close()
		}

		done <- true
	}()

	events := agent.blockReporter.readTraceEvents(1000)
	/*fmt.Printf("EVENTS:\n")
	  for _, ev := range events {
	    for _, f := range ev.Stk {
	      fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
	    }
	    fmt.Printf("\n")
	  }*/

	entryFilterFunc := func(funcName string) bool {
		return strings.Contains(funcName, "net/http.(*Server).Serve")
	}

	eventIndex := agent.blockReporter.nextEntry(events, entryFilterFunc, 0)
	if eventIndex < 0 {
		t.Error("Entry not found for request 1")
		return
	}

	eventIndex = agent.blockReporter.nextEntry(events, entryFilterFunc, eventIndex+1)
	if eventIndex < 0 {
		t.Error("Entry not found for request 2")
		return
	}

	entry := events[eventIndex]

	selectedEvents := selectEventsByTrace(entry)
	callGraph, err := agent.blockReporter.createBlockCallGraph(selectedEvents, nil, 1000)
	if err != nil {
		t.Error(err)
		return
	}
	//fmt.Printf("WAIT TIME: %v\n", callGraph.measurement)
	//fmt.Printf("CALL GRAPH: %v\n", callGraph.printLevel(0))
	if callGraph.measurement < 100 {
		t.Error("Wait time is too low")
	}

	if !strings.Contains(fmt.Sprintf("%v", callGraph.toMap()), "TestCreateBlockCallGraphWithTrace") {
		t.Error("The test function is not found in the profile")
	}

	<-done
}

func TestAppendToTraceList(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	traceList := newBreakdownNode("root")

	callGraph := newBreakdownNode("1")
	callGraph.measurement = 1
	agent.blockReporter.appendToTraceList(traceList, callGraph, 2)

	callGraph = newBreakdownNode("2")
	callGraph.measurement = 2
	agent.blockReporter.appendToTraceList(traceList, callGraph, 2)

	callGraph = newBreakdownNode("3")
	callGraph.measurement = 3
	agent.blockReporter.appendToTraceList(traceList, callGraph, 2)

	if len(traceList.children) != 2 {
		t.Error("wrong number of children")
	}

	if traceList.findChild("1") != nil {
		t.Error("child 1 should not be in the list")
	}

	if traceList.findChild("2") == nil {
		t.Error("child 2 should be in the list")
	}

	if traceList.findChild("3") == nil {
		t.Error("child 2 should be in the list")
	}
}
