package main

import (
	"fmt"
	"net/http"

	"github.com/stackimpact/stackimpact-go"
)

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello world!")
}

func main() {
	agent := stackimpact.Start(stackimpact.Options{
		AgentKey:       "agent key here",
		AppName:        "Basic Go Server",
		AppVersion:     "1.0.0",
		AppEnvironment: "production",
	})

	http.HandleFunc(agent.ProfileHandlerFunc("/", handler))
	http.ListenAndServe(":8080", nil)
}
