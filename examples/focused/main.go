package main

import (
	"fmt"
	"net/http"

	"github.com/stackimpact/stackimpact-go"
)

var agent *stackimpact.Agent

func helloHandler(w http.ResponseWriter, r *http.Request) {
	span := agent.ProfileWithName("Hello handler")
	defer span.Stop()

	fmt.Fprintf(w, "Hello world!")
}

func main() {
	agent = stackimpact.Start(stackimpact.Options{
		AgentKey: "agent key here",
		AppName: "My Go App",
	})

	http.HandleFunc("/", helloHandler) 
	http.ListenAndServe(":8080", nil)
}
