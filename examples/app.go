package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/stackimpact/stackimpact-go"
)

func useCPU(duration int, usage int) {
	for j := 0; j < duration; j++ {
		go func() {
			for i := 0; i < usage*80000; i++ {
				str := "str" + strconv.Itoa(i)
				str = str + "a"
			}
		}()

		time.Sleep(1 * time.Second)
	}
}

func simulateCPUUsage() {
	// sumulate CPU usage anomaly - every 45 minutes
	cpuAnomalyTicker := time.NewTicker(45 * time.Minute)
	go func() {
		for {
			select {
			case <-cpuAnomalyTicker.C:
				// for 60 seconds produce generate 50% CPU usage
				useCPU(60, 50)
			}
		}
	}()

	// generate constant ~10% CPU usage
	useCPU(math.MaxInt64, 10)
}

func leakMemory(duration int, size int) {
	mem := make([]string, 0)

	for j := 0; j < duration; j++ {
		go func() {
			for i := 0; i < size/duration*50000; i++ {
				mem = append(mem, string(i))
			}
		}()

		time.Sleep(1 * time.Second)
	}
}

func simulateMemoryLeak() {
	// simulate memory leak - every 30 minutes
	anomalyTicker := time.NewTicker(30 * time.Minute)
	go func() {
		for {
			select {
			case <-anomalyTicker.C:
				leakMemory(60, 100)
			}
		}
	}()

	// simulate memory leak - constantly
	constantTicker := time.NewTicker(60 * time.Second)
	go func() {
		for {
			select {
			case <-constantTicker.C:
				leakMemory(60, 10)
			}
		}
	}()

	go leakMemory(60, 10)
}


func simulateChannelWait() {
	for {
		done := make(chan bool)

		go func() {
			wait := make(chan bool)

			go func() {
				time.Sleep(500 * time.Millisecond)

				wait <- true
			}()

			<-wait

			done <- true
		}()

		<-done

		time.Sleep(500 * time.Millisecond)
	}
}

func simulateNetworkWait() {
	// start test server
	go func() {
		http.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(500 * time.Millisecond)
			fmt.Fprintf(w, "OK")
		})

		if err := http.ListenAndServe(":5000", nil); err != nil {
			log.Fatal(err)
			return
		}
	}()

	for {
		done := make(chan bool)

		go func() {
			res, err := http.Get("http://localhost:5000/test")
			if err != nil {
				log.Fatal(err)
			} else {
				defer res.Body.Close()
			}

			done <- true
		}()

		time.Sleep(500 * time.Millisecond)

		<-done
	}
}

func simulateSyscallWait() {
	for {
		done := make(chan bool)

		go func() {
			_, err := exec.Command("sleep", "1").Output()
			if err != nil {
				log.Fatal(err)
			}

			done <- true
		}()

		time.Sleep(1 * time.Second)

		<-done
	}
}

func simulateLockWait() {
	for {
		done := make(chan bool)

		lock := &sync.Mutex{}
		lock.Lock()

		go func() {
			lock.Lock()

			done <- true
		}()

		go func() {
			time.Sleep(500 * time.Millisecond)
			lock.Unlock()
		}()

		<-done

		time.Sleep(500 * time.Millisecond)
	}
}

func main() {
	// StackImpact initialization
	agent := stackimpact.NewAgent()
	agent.Debug = true
	agent.Configure(os.Getenv("AGENT_KEY"), "ExampleGoApp")
	// end StackImpact initialization

	// Overwrite dashboard (onprem only)
	if os.Getenv("DASHBOARD_ADDRESS") != "" {
		agent.DashboardAddress = os.Getenv("DASHBOARD_ADDRESS")
	}

	go simulateCPUUsage()
	go simulateMemoryLeak()
	go simulateChannelWait()
	go simulateNetworkWait()
	go simulateSyscallWait()
	go simulateLockWait()

	select {}
}
