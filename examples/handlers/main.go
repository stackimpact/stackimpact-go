package main

import (
	"fmt"
	"math/rand"
	"net/http"

	"github.com/stackimpact/stackimpact-go"
)

func requestHandler(w http.ResponseWriter, r *http.Request) {
	// simulate cpu work
	for i := 0; i < 10000000; i++ {
		rand.Intn(1000)
	}

	fmt.Fprintf(w, "Done")
}

func main() {
	// Initialize StackImpact agent
	agent := stackimpact.Start(stackimpact.Options{
		AgentKey: "agent key yere",
		AppName:  "Workload Profiling Example",
	})

	// Serve
	http.HandleFunc(agent.ProfileHandlerFunc("/test", requestHandler))
	http.ListenAndServe(":9000", nil)
}
