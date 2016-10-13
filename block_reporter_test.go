package stackimpact

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	pprofTrace "github.com/stackimpact/stackimpact-go/pprof/trace"
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

	events := agent.blockReporter.readTraceProfile(1000)

	/*fmt.Printf("EVENTS:\n")
	  for _, ev := range events {
	    for _, f := range ev.Stk {
	      fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
	    }
	    fmt.Printf("\n")
	  }*/

	callGraph, err := agent.blockReporter.createBlockCallGraph(events, pprofTrace.EvGoBlockRecv, nil, 1000)
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

	events := agent.blockReporter.readTraceProfile(1000)

	/*fmt.Printf("EVENTS:\n")
	  for _, ev := range events {
	    for _, f := range ev.Stk {
	      fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
	    }
	    fmt.Printf("\n")
	  }*/

	callGraph, err := agent.blockReporter.createBlockCallGraph(events, pprofTrace.EvGoBlockNet, nil, 1000)
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

	events := agent.blockReporter.readTraceProfile(1000)

	/*fmt.Printf("EVENTS:\n")
	  for _, ev := range events {
	    for _, f := range ev.Stk {
	      fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
	    }
	    fmt.Printf("\n")
	  }*/

	callGraph, err := agent.blockReporter.createBlockCallGraph(events, pprofTrace.EvGoBlockSync, nil, 1000)
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

	events := agent.blockReporter.readTraceProfile(2000)

	/*fmt.Printf("EVENTS:\n")
	  for _, ev := range events {
	    for _, f := range ev.Stk {
	      fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
	    }
	    fmt.Printf("\n")
	  }*/

	callGraph, err := agent.blockReporter.createBlockCallGraph(events, pprofTrace.EvGoSysCall, nil, 2000)
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
