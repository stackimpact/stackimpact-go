# StackImpact Go Agent

## Description

StackImpact is a performance profiling and monitoring service for production Go applications. It gives you continuous visibility with line-of-code precision into application performance, such as CPU, memory and IO hot spots as well execution bottlenecks, allowing to optimize applications and troubleshoot issues before they impact customers. Learn more at [stackimpact.com](https://stackimpact.com/).

Main features:

* Automatic hot spot profiling for CPU, memory allocations, network, system calls and lock contention.
* Automatic bottleneck tracing for HTTP handlers and HTTP clients.
* Health monitoring including CPU, Memory, garbage collection and other runtime metrics.
* Alerts on hot spot anomalies.
* Multiple account users for team collaboration.

Learn more on [features](https://stackimpact.com/features/) page (with screenshots).


StackImpact agent (this package) reports performance information to the Dashboard running as SaaS or On-Premise.


## Requirements

Linux or OS X. Go version 1.5+.


## Getting started

#### Create StackImpact account

Sign up for a free account at [stackimpact.com](https://stackimpact.com/).


#### Installing the agent

Install the Go agent by running

```
go get github.com/stackimpact/stackimpact-go
```

And import the package `github.com/stackimpact/stackimpact-go` in your application.


#### Configuring the agent

Start the agent by specifying agent key and application name. The agent key can be found in your account's Configuration section.

```go
agent := stackimpact.NewAgent();
agent.Configure("agent key here", "MyGoApp")
```

Other initialization options are (set before calling Configure):
* `agent.DashboardAddress` (Optional) Used by on-premises deployments only.
* `agent.HostName` (Optional) By default host name will be the OS hostname.
* `agent.Debug` (Optional) Enables debug logging.


Example:

```go
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
  	agent := stackimpact.NewAgent()
  	agent.Configure("agent key here", "Basic Go Server")

  	http.HandleFunc("/", handler)
    http.ListenAndServe(":8080", nil)
}
```


#### Analyzing performance data in the Dashboard

Once your application is restarted, start observing regular and anomaly-triggered CPU, memory, IO, and other hot spot profiles, execution bottlenecks as well as process metrics in the [Dashboard](https://dashboard.stackimpact.com/).


## Troubleshooting

To enable debug logging, add `agent.Debug = true` to startup options. If debug log doesn't give you any hints on how to fix a problem, please report it to our support team in your account's Support section.


## Overhead
Reporting CPU, network and system profiles requires regular and anomaly-triggered profiling and tracing activation for short periods of time. Unlike memory profiling and process-level metric reporting, they produce some overhead when active. The agent makes sure the overhead stays within 2% and has no effect on application in any way.
