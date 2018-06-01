package main

import (
	// stdlib
	"context"
	"fmt"
	"os"

	// external
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/kit/sd/etcd"
	uuid "github.com/satori/go.uuid"

	// project
	feclient "github.com/basvanbeek/opencensus-gokit-example/services/cli/transport/clients/frontend"
	"github.com/basvanbeek/opencensus-gokit-example/services/event"
	"github.com/basvanbeek/opencensus-gokit-example/services/frontend"
)

const (
	serviceName = "frontend-cli"
)

func main() {
	var (
		err      error
		instance = uuid.Must(uuid.NewV4())
	)

	// initialize our structured logger for the service
	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(os.Stderr)
		logger = log.NewSyncLogger(logger)
		logger = level.NewFilter(logger, level.AllowDebug())
		logger = log.With(logger,
			"svc", serviceName,
			"instance", instance,
			"ts", log.DefaultTimestampUTC,
			"clr", log.DefaultCaller,
		)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sdc etcd.Client
	{
		// create our Go kit etcd client
		sdc, err = etcd.NewClient(ctx, []string{"http://localhost:2379"}, etcd.ClientOptions{})
		if err != nil {
			level.Error(logger).Log("exit", err)
			os.Exit(-1)
		}
	}

	var client frontend.Service
	{
		// create an instancer for the event client
		instancer, err := etcd.NewInstancer(sdc, "/services/"+event.ServiceName+"/http", logger)
		if err != nil {
			level.Error(logger).Log("exit", err)
		}

		client = feclient.NewHTTP(instancer, logger)
	}

	details, err := client.Login(ctx, "john", "doe")
	fmt.Printf("\nCLIENT LOGIN:\nRES:%+v\nERR: %+v\n\n", details, err)

	details, err = client.Login(ctx, "jane", "doe")
	fmt.Printf("\nCLIENT LOGIN:\nRES:%+v\nERR: %+v\n\n", details, err)

	details, err = client.Login(ctx, "Anonymous", "Coward")
	fmt.Printf("\nCLIENT LOGIN:\nRES:%+v\nERR: %+v\n\n", details, err)

}
