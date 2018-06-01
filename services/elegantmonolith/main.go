package main

import (
	// stdlib
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// external
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/kit/sd/etcd"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/oklog/run"
	uuid "github.com/satori/go.uuid"

	// project
	"github.com/basvanbeek/opencensus-gokit-example/services/device"
	devsql "github.com/basvanbeek/opencensus-gokit-example/services/device/database/sqlite"
	devimplementation "github.com/basvanbeek/opencensus-gokit-example/services/device/implementation"
	"github.com/basvanbeek/opencensus-gokit-example/services/event"
	evtsql "github.com/basvanbeek/opencensus-gokit-example/services/event/database/sqlite"
	evtimplementation "github.com/basvanbeek/opencensus-gokit-example/services/event/implementation"
	"github.com/basvanbeek/opencensus-gokit-example/services/frontend"
	feimplementation "github.com/basvanbeek/opencensus-gokit-example/services/frontend/implementation"
	"github.com/basvanbeek/opencensus-gokit-example/services/frontend/transport"
	fesvchttp "github.com/basvanbeek/opencensus-gokit-example/services/frontend/transport/http"
	"github.com/basvanbeek/opencensus-gokit-example/services/qr"
	qrimplementation "github.com/basvanbeek/opencensus-gokit-example/services/qr/implementation"
	"github.com/basvanbeek/opencensus-gokit-example/shared/network"
)

const (
	serviceName = "ElegantMonolith"
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

	level.Info(logger).Log("msg", "service started")
	defer level.Info(logger).Log("msg", "service ended")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create our etcd client for Service Discovery
	//
	// we could have used the v3 client but then we must vendor or suffer the
	// following issue originating from gRPC init:
	// panic: http: multiple registrations for /debug/requests
	var sdc etcd.Client
	{
		// create our Go kit etcd client
		sdc, err = etcd.NewClient(ctx, []string{"http://localhost:2379"}, etcd.ClientOptions{})
		if err != nil {
			level.Error(logger).Log("exit", err)
			os.Exit(-1)
		}
	}

	// Create our DB Connection Driver
	var db *sqlx.DB
	{
		db, err = sqlx.Open("sqlite3", "monolith.db")
		if err != nil {
			level.Error(logger).Log("exit", err)
			os.Exit(-1)
		}

		// make sure the DB is in WAL mode
		if _, err = db.Exec(`PRAGMA journal_mode=wal`); err != nil {
			level.Error(logger).Log("exit", err)
			os.Exit(-1)
		}
	}

	// Create our Event service component
	var eventService event.Service
	{
		var logger = log.With(logger, "component", event.ServiceName)

		repository, err := evtsql.New(db, logger)
		if err != nil {
			level.Error(logger).Log("exit", err)
			os.Exit(-1)
		}
		eventService = evtimplementation.NewService(repository, logger)
		// add service level middlewares here

	}

	// Create our Device service component
	var deviceService device.Service
	{
		var logger = log.With(logger, "component", device.ServiceName)

		repository, err := devsql.New(db, logger)
		if err != nil {
			level.Error(logger).Log("exit", err)
			os.Exit(-1)
		}
		deviceService = devimplementation.NewService(repository, logger)
		// add service level middlewares here
	}

	// Create our QR service component
	var qrService qr.Service
	{
		var logger = log.With(logger, "component", qr.ServiceName)

		qrService = qrimplementation.NewService(logger)
		// add service level middlewares here
	}

	// Create our frontend service component
	var frontendService frontend.Service
	{
		var logger = log.With(logger, "component", frontend.ServiceName)

		frontendService = feimplementation.NewService(
			eventService, deviceService, qrService, logger,
		)
		// add service level middlewares here
	}

	var endpoints transport.Endpoints
	{
		endpoints = transport.MakeEndpoints(frontendService)
		// add frontend endpoint level middlewares here
	}

	// run.Group manages our goroutine lifecycles
	// see: https://www.youtube.com/watch?v=LHe1Cb_Ud_M&t=15m45s
	var g run.Group
	{
		// set-up our http transport
		var (
			bindIP, _       = network.HostIP()
			frontendService = fesvchttp.NewHTTPHandler(endpoints, logger)
			listener, _     = net.Listen("tcp", bindIP+":0")                                       // dynamic port assignment
			svcInstance     = fmt.Sprintf("/services/%s/http/%s/", frontend.ServiceName, instance) // monolith is basically frontend service but with all micro service backend logic embedded
			addr            = "http://" + listener.Addr().String()
			ttl             = etcd.NewTTLOption(3*time.Second, 10*time.Second)
			service         = etcd.Service{Key: svcInstance, Value: addr, TTL: ttl}
			registrar       = etcd.NewRegistrar(sdc, service, logger)
		)

		g.Add(func() error {
			registrar.Register()
			return http.Serve(listener, frontendService)
		}, func(error) {
			registrar.Deregister()
			listener.Close()
		})
	}
	{
		// set-up our signal handler
		var (
			cancelInterrupt = make(chan struct{})
			c               = make(chan os.Signal, 2)
		)
		defer close(c)

		g.Add(func() error {
			signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
			select {
			case sig := <-c:
				return fmt.Errorf("received signal %s", sig)
			case <-cancelInterrupt:
				return nil
			}
		}, func(error) {
			close(cancelInterrupt)
		})
	}

	// spawn our goroutines and wait for shutdown
	level.Error(logger).Log("exit", g.Run())
}