package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/stackimpact/stackimpact-go"
)

var agent *stackimpact.Agent

func handlerA(w http.ResponseWriter, r *http.Request) {
	res, err := http.Get("https://nonexistingdomain")
	if err != nil {
		agent.RecordError(err)
	} else {
		defer res.Body.Close()
	}

	fmt.Fprintf(w, "Done")
}

func handlerB(w http.ResponseWriter, r *http.Request) {
	defer agent.RecordPanic()

	s := []string{"a", "b"}
	fmt.Println(s[2]) // this will cause panic

	fmt.Fprintf(w, "Done")
}

func handlerC(w http.ResponseWriter, r *http.Request) {
	defer agent.RecordAndRecoverPanic()

	s := []string{"a", "b"}
	fmt.Println(s[2]) // this will cause panic

	fmt.Fprintf(w, "Done")
}

func main() {
	// StackImpact initialization
	agent = stackimpact.Start(stackimpact.Options{
		AgentKey: "0dac7f9b26b07a7c25328f7e567e4b89409e4ac3",
		AppName:  "Some Go App",
	})
	// end StackImpact initialization

	// Start server
	go func() {
		http.HandleFunc("/a", handlerA)
		http.HandleFunc("/b", handlerB)
		http.HandleFunc("/c", handlerC)
		http.ListenAndServe(":9000", nil)
	}()

	requestTicker := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-requestTicker.C:
			if rand.Intn(2) == 0 {
				res, err := http.Get("http://localhost:9000/a")
				if err == nil {
					res.Body.Close()
				}
			}

			if rand.Intn(3) == 0 {
				res, err := http.Get("http://localhost:9000/b")
				if err == nil {
					res.Body.Close()
				}
			}

			if rand.Intn(4) == 0 {
				res, err := http.Get("http://localhost:9000/c")
				if err == nil {
					res.Body.Close()
				}
			}
		}
	}
}
