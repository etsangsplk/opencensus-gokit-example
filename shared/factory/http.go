package factory

import (
	// stdlib
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	// external
	"github.com/go-kit/kit/circuitbreaker"
	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/sd"
	"github.com/go-kit/kit/sd/lb"
	kitoc "github.com/go-kit/kit/tracing/opencensus"
	kithttp "github.com/go-kit/kit/transport/http"
	"github.com/gorilla/mux"
	"github.com/sony/gobreaker"
	"go.opencensus.io/trace"

	// project
	"github.com/basvanbeek/opencensus-gokit-example/shared/errormw"
	"github.com/basvanbeek/opencensus-gokit-example/shared/oc"
)

// CreateHTTPEndpoint creates a Go kit client endpoint
func CreateHTTPEndpoint(
	instancer sd.Instancer, middleware endpoint.Middleware, operationName string,
	encodeRequest kithttp.EncodeRequestFunc,
	decodeResponse kithttp.DecodeResponseFunc,
) endpoint.Endpoint {
	options := []kithttp.ClientOption{
		kitoc.HTTPClientTrace(), // OpenCensus HTTP Client transport tracing
	}

	// factory is called each time a new instance is received from service
	// discovery. it will create a new Go kit client endpoint which will be
	// consumed by the endpointer logic.
	factory := func(instance string) (endpoint.Endpoint, io.Closer, error) {
		baseURL, err := url.Parse(instance)
		if err != nil {
			// invalid instance string received... can't build endpoint
			return nil, nil, err
		}

		// set-up our Go kit client endpoint
		// method is not set yet as it will be decided by the provided route
		// when encoding the request using our request encoder.
		clientEndpoint := kithttp.NewClient(
			"", baseURL, encodeRequest, decodeResponse, options...,
		).Endpoint()

		// configure per instance circuit breaker middleware
		cb := circuitbreaker.Gobreaker(
			gobreaker.NewCircuitBreaker(gobreaker.Settings{
				MaxRequests: 5,
				Interval:    10 * time.Second,
				Timeout:     10 * time.Second,
				ReadyToTrip: func(counts gobreaker.Counts) bool {
					return counts.ConsecutiveFailures > 5
				},
			}),
		)

		// middleware to trace our client endpoint
		tr := oc.ClientEndpoint(
			operationName, trace.StringAttribute("peer.address", instance),
		)

		// chain our middlewares and wrap our endpoint
		clientEndpoint = endpoint.Chain(cb, tr, middleware)(clientEndpoint)

		return clientEndpoint, nil, nil
	}

	// endpoints manages the list of available endpoints servicing our method
	endpoints := sd.NewEndpointer(instancer, factory, log.NewNopLogger())

	// balancer can do a random pick from the endpoint list
	balancer := lb.NewRandom(endpoints, time.Now().UnixNano())

	var (
		count   = 3
		timeout = 5 * time.Second
	)

	// retry uses balancer for executing a method call with retry and
	// timeout logic so client consumer does not have to think about it.
	endpoint := lb.Retry(count, timeout, balancer)

	// wrap our retries in an annotated parent span
	endpoint = oc.RetryEndpoint(operationName, oc.Random, count, timeout)(endpoint)

	// unwrap business logic errors
	endpoint = errormw.UnwrapError(log.NewNopLogger())(endpoint)

	// return our endpoint
	return endpoint
}

// EncodeGenericRequest is a generic request encoder which can be used if we
// don't have to deal with URL parameters.
func EncodeGenericRequest(route *mux.Route) kithttp.EncodeRequestFunc {
	return func(_ context.Context, r *http.Request, request interface{}) error {
		var err error

		if r.URL, err = route.Host(r.URL.Host).URL(); err != nil {
			return err
		}
		if methods, err := route.GetMethods(); err == nil {
			r.Method = methods[0]
		}

		if request != nil {
			var buf bytes.Buffer
			if err := json.NewEncoder(&buf).Encode(request); err != nil {
				return err
			}
			r.Body = ioutil.NopCloser(&buf)
		}
		return nil
	}
}
