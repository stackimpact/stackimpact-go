package main

import (
	"time"
	"math/rand"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/stackimpact/stackimpact-go"
)

var agent *stackimpact.Agent

var mem []string = make([]string, 0)

func Handler(request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// profile this handler
	span := agent.Profile()
	defer span.Stop()

	// periodically send profiles to the sashboard (not on every request)
	defer agent.Report()

	// simulate cpu work
	for i := 0; i < 1000000; i++ {
		rand.Intn(1000)
	}

	// simulate memory leak
	for i := 0; i < 1000; i++ {
		mem = append(mem, string(i))
	}

	// simulate blocking call
	done := make(chan bool)
	go func() {
		time.Sleep(200 * time.Millisecond)
		done <- true
	}()
	<-done

	return events.APIGatewayProxyResponse{
		Body:       "Hello",
		StatusCode: 200,
	}, nil

}

func main() {
	agent = stackimpact.Start(stackimpact.Options{
		AgentKey: "agent key here",
		AppName:  "LambdaGo",
		DisableAutoProfiling: true,
	})

	lambda.Start(Handler)
}
