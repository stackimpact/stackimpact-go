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

func TestAdjustTraceDurtaion(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	duration := agent.blockReporter.adjustTraceDuration()

	if duration < 3000 {
		t.Errorf("Duration should be >= 3000, but is %v", duration)
	}

	done := make(chan bool)

	go func() {
		for i := 0; i < 10000; i++ {
			go func() {
				<-done
			}()
		}
	}()

	duration = agent.blockReporter.adjustTraceDuration()

	done <- true

	if duration != 100 {
		t.Errorf("Duration should be 100, but is %v", duration)
	}
}

func TestCreateBlockCallGraphWithChannel(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	done := make(chan bool)

	go func() {
		time.Sleep(50 * time.Millisecond)

		wait := make(chan bool)

		go func() {
			time.Sleep(100 * time.Millisecond)

			wait <- true
		}()

		<-wait

		done <- true
	}()

	events := agent.blockReporter.readTraceEvents(200)

	/*fmt.Printf("EVENTS:\n")
	  for _, ev := range events {
	    for _, f := range ev.Stk {
	      fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
	    }
	    fmt.Printf("\n")
	  }*/

	selectorFunc := func(event *pprofTrace.Event) bool {
		return (event.Type == pprofTrace.EvGoBlockRecv)
	}
	selectedEvents := selectEvents(events, selectorFunc)
	callGraph, err := agent.blockReporter.createBlockCallGraph(selectedEvents, nil, 200)
	if err != nil {
		t.Error(err)
		return
	}
	callGraph.propagate()
	//fmt.Printf("WAIT TIME: %v\n", callGraph.measurement)
	//fmt.Printf("CALL GRAPH: %v\n", callGraph.printLevel(0))
	if callGraph.measurement < 50 {
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
		http.HandleFunc("/ready1", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "OK")
		})

		http.HandleFunc("/test1", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			fmt.Fprintf(w, "OK")
		})

		if err := http.ListenAndServe(":5001", nil); err != nil {
			t.Error(err)
			return
		}
	}()

	waitForServer("http://localhost:5001/ready1")

	go func() {
		time.Sleep(10 * time.Millisecond)
		res, err := http.Get("http://localhost:5001/test1")
		if err != nil {
			t.Error(err)
		} else {
			defer res.Body.Close()
		}
	}()

	events := agent.blockReporter.readTraceEvents(150)

	/*fmt.Printf("EVENTS:\n")
	  for _, ev := range events {
	    for _, f := range ev.Stk {
	      fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
	    }
	    fmt.Printf("\n")
	  }*/

	selectorFunc := func(event *pprofTrace.Event) bool {
		return (event.Type == pprofTrace.EvGoBlockNet)
	}
	selectedEvents := selectEvents(events, selectorFunc)
	callGraph, err := agent.blockReporter.createBlockCallGraph(selectedEvents, nil, 150)
	if err != nil {
		t.Error(err)
		return
	}
	callGraph.propagate()
	//fmt.Printf("WAIT TIME: %v\n", callGraph.measurement)
	//fmt.Printf("CALL GRAPH: %v\n", callGraph.printLevel(0))
	if callGraph.measurement < 50 {
		t.Error("Wait time is too low")
	}

	if !strings.Contains(fmt.Sprintf("%v", callGraph.toMap()), "net.(*netFD).Read") {
		t.Error("The test function is not found in the profile")
	}
}

func TestCreateBlockCallGraphWithLock(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	done := make(chan bool)

	lock := &sync.Mutex{}
	lock.Lock()

	go func() {
		time.Sleep(50 * time.Millisecond)
		lock.Lock()

		done <- true
	}()

	go func() {
		time.Sleep(100 * time.Millisecond)
		lock.Unlock()
	}()

	events := agent.blockReporter.readTraceEvents(200)

	/*fmt.Printf("EVENTS:\n")
	  for _, ev := range events {
	    for _, f := range ev.Stk {
	      fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
	    }
	    fmt.Printf("\n")
	  }*/

	selectorFunc := func(event *pprofTrace.Event) bool {
		return (event.Type == pprofTrace.EvGoBlockSync)
	}
	selectedEvents := selectEvents(events, selectorFunc)
	callGraph, err := agent.blockReporter.createBlockCallGraph(selectedEvents, nil, 200)
	if err != nil {
		t.Error(err)
		return
	}
	callGraph.propagate()
	//fmt.Printf("WAIT TIME: %v\n", callGraph.measurement)
	//fmt.Printf("CALL GRAPH: %v\n", callGraph.printLevel(0))
	if callGraph.measurement < 50 {
		t.Error("Wait time is too low")
	}

	if !strings.Contains(fmt.Sprintf("%v", callGraph.toMap()), "TestCreateBlockCallGraphWithLock") {
		t.Error("The test function is not found in the profile")
	}
}

func TestCreateBlockCallGraphWithSyscall(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	go func() {
		time.Sleep(50 * time.Millisecond)
		_, err := exec.Command("sleep", "0.1").Output()
		if err != nil {
			t.Error(err)
		}
	}()

	events := agent.blockReporter.readTraceEvents(200)

	/*fmt.Printf("EVENTS:\n")
	  for _, ev := range events {
	    for _, f := range ev.Stk {
	      fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
	    }
	    fmt.Printf("\n")
	  }*/

	selectorFunc := func(event *pprofTrace.Event) bool {
		return (event.Type == pprofTrace.EvGoSysCall)
	}
	selectedEvents := selectEvents(events, selectorFunc)
	callGraph, err := agent.blockReporter.createBlockCallGraph(selectedEvents, nil, 200)
	if err != nil {
		t.Error(err)
		return
	}
	callGraph.propagate()
	//fmt.Printf("WAIT TIME: %v\n", callGraph.measurement)
	//fmt.Printf("CALL GRAPH: %v\n", callGraph.printLevel(0))
	if callGraph.measurement < 50 {
		t.Error("Wait time is too low")
	}

	if !strings.Contains(fmt.Sprintf("%v", callGraph.toMap()), "TestCreateBlockCallGraphWithSyscall") {
		t.Error("The test function is not found in the profile")
	}
}

func TestCreateBlockCallGraphWithHTTPHandler(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	// start HTTP server
	go func() {
		http.HandleFunc("/ready2", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "OK")
		})

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

	waitForServer("http://localhost:5002/ready2")

	go func() {
		time.Sleep(50 * time.Millisecond)

		res, err := http.Get("http://localhost:5002/test2")
		if err != nil {
			t.Error(err)
		} else {
			defer res.Body.Close()
		}
	}()

	events := agent.blockReporter.readTraceEvents(300)

	/*for _, ev := range events {
		fmt.Printf("\n\n\n\nEVENTS:\n")
		lev := ev
		for lev != nil {
			fmt.Printf("\n\nLINKED EVENT %v (%v):\n", pprofTrace.EventDescriptions[lev.Type], lev.G)
			for _, f := range lev.Stk {
				fmt.Printf("%v (%v:%v)\n", f.Fn, f.File, f.Line)
			}

			lev = lev.Link
		}
	}*/

	selectorFunc := func(event *pprofTrace.Event) bool {
		switch event.Type {
		case
			pprofTrace.EvGoBlockNet,
			pprofTrace.EvGoSysCall,
			pprofTrace.EvGoBlockSend,
			pprofTrace.EvGoBlockRecv,
			pprofTrace.EvGoBlockSelect,
			pprofTrace.EvGoBlockSync,
			pprofTrace.EvGoBlockCond,
			pprofTrace.EvGoSleep:
		default:
			return false
		}

		l := len(event.Stk)
		return (l >= 2 &&
			event.Stk[l-1].Fn == "net/http.(*conn).serve" &&
			event.Stk[l-2].Fn == "net/http.serverHandler.ServeHTTP")
	}
	selectedEvents := selectEvents(events, selectorFunc)
	callGraph, err := agent.blockReporter.createTraceCallGraph(selectedEvents)
	if err != nil {
		t.Error(err)
		return
	}
	callGraph.evaluateP95()
	callGraph.propagate()

	//fmt.Printf("WAIT TIME: %v\n", callGraph.measurement)
	//fmt.Printf("CALL GRAPH: %v\n", callGraph.printLevel(0))

	if callGraph.measurement < 50 {
		t.Error("Wait time is too low")
	}

	if !strings.Contains(fmt.Sprintf("%v", callGraph.toMap()), "TestCreateBlockCallGraphWithHTTPHandler.func1") {
		t.Error("The test function is not found in the profile")
	}
}

func TestCreateBlockCallGraphWithHTTPClient(t *testing.T) {
	agent := NewAgent()
	agent.Debug = true

	// start HTTP server
	go func() {
		http.HandleFunc("/ready3", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "OK")
		})

		http.HandleFunc("/test3", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)

			fmt.Fprintf(w, "OK")
		})

		if err := http.ListenAndServe(":5003", nil); err != nil {
			t.Error(err)
			return
		}
	}()

	waitForServer("http://localhost:5003/ready3")

	go func() {
		time.Sleep(50 * time.Millisecond)

		// request
		res, err := http.Get("http://localhost:5003/test3")
		if err != nil {
			t.Error(err)
		} else {
			defer res.Body.Close()
		}
	}()

	events := agent.blockReporter.readTraceEvents(200)

	selectorFunc := func(event *pprofTrace.Event) bool {
		switch event.Type {
		case
			pprofTrace.EvGoBlockNet,
			pprofTrace.EvGoSysCall,
			pprofTrace.EvGoBlockSend,
			pprofTrace.EvGoBlockRecv,
			pprofTrace.EvGoBlockSelect,
			pprofTrace.EvGoBlockSync,
			pprofTrace.EvGoBlockCond,
			pprofTrace.EvGoSleep:
		default:
			return false
		}

		for _, f := range event.Stk {
			if f.Fn == "net/http.(*Client).send" {
				return true
			}
		}

		return false
	}
	selectedEvents := selectEvents(events, selectorFunc)
	callGraph, err := agent.blockReporter.createTraceCallGraph(selectedEvents)
	if err != nil {
		t.Error(err)
		return
	}
	callGraph.evaluateP95()
	callGraph.propagate()

	//fmt.Printf("WAIT TIME: %v\n", callGraph.measurement)
	//fmt.Printf("CALL GRAPH: %v\n", callGraph.printLevel(0))

	if callGraph.measurement < 50 {
		t.Error("Wait time is too low")
	}

	if !strings.Contains(fmt.Sprintf("%v", callGraph.toMap()), "TestCreateBlockCallGraphWithHTTPClient.func2") {
		t.Error("The test function is not found in the profile")
	}
}

func waitForServer(url string) {
	for {
		if _, err := http.Get(url); err == nil {
			time.Sleep(10 * time.Millisecond)
			break
		}
	}
}
