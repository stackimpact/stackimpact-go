package main

import (
	"math/rand"

	"github.com/stackimpact/stackimpact-go"
)


func main() {
	agent := stackimpact.Start(stackimpact.Options{
		AgentKey: "agent key here",
		AppName: "My Go App",
		AppEnvironment: "mydevenv",
		DisableAutoProfiling: true,
	})

	agent.StartCPUProfiler()

	// simulate CPU work
	for i := 0; i < 100000000; i++ {
		rand.Intn(1000)
	}

	agent.StopCPUProfiler()
}
