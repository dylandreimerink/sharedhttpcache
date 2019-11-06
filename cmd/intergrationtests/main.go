package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/dylandreimerink/sharedhttpcache"
)

var Scenarios []IntergrationTestScenario

func main() {

	originServerHandler := &OriginServerHandler{
		RequestChannel: make(chan *http.Request),
	}

	originServer := &http.Server{
		Handler: originServerHandler,
	}

	originListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	fmt.Println("Starting origin server on port:", originListener.Addr().(*net.TCPAddr).Port)

	//Start the origin server in a separate thread
	go func() {
		panic(originServer.Serve(originListener))
	}()

	cachingServer := &http.Server{}

	cachingListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	fmt.Println("Starting caching server on port:", cachingListener.Addr().(*net.TCPAddr).Port)

	//Start the origin server in a separate thread
	go func() {
		panic(cachingServer.Serve(cachingListener))
	}()

	for _, scenario := range Scenarios {
		fmt.Println("Testing scenario: ", scenario.Name)

		//Overwrite the default forward config to the origin server since the port is random every time
		scenario.Controller.DefaultForwardConfig = &sharedhttpcache.ForwardConfig{
			Host: originListener.Addr().String(),
		}

		cachingServer.Handler = scenario.Controller

		for _, step := range scenario.Steps {

			fmt.Println("Testing step: ", step.Name)

			//Set the correct handler for this step
			originServerHandler.ContentHandler = step.OriginHandler

			//Make a channel with which the origin server checker routine can communicate to the main thread
			originErrorChannel := make(chan error)

			//Wait for a request at the origin server for 5 seconds
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			go func() {
				select {
				case request := <-originServerHandler.RequestChannel:
					//If we get a request check if we expected one
					if step.ExpectRequestToOrigin {
						//If we do we check if the content is expected

						originErrorChannel <- step.CacheRequestChecker.RequestExpected(request)
					} else {
						//If not give a error

						//TODO print the request
						originErrorChannel <- errors.New("Received request from caching server while not expecting one")
					}
				case <-ctx.Done():
					//If we don't receive a request before the time runs out we check if we expect that
					if step.ExpectRequestToOrigin {
						//If no give a error
						originErrorChannel <- errors.New("Expected request from caching server to origin server but never received any")
					} else {
						//If so response with no error
						originErrorChannel <- nil
					}
				}
			}()

			step.ClientRequest.URL.Scheme = "http"
			step.ClientRequest.URL.Host = cachingListener.Addr().String()

			//Send the request to the caching server and wait for a response
			response, err := http.DefaultClient.Do(step.ClientRequest)
			if err != nil {
				fmt.Printf("Scenario '%s' failed on step '%s', got error while sending request to caching server: '%s'\n", scenario.Name, step.Name, err.Error())
				os.Exit(1)
			}

			//If we don't expect a request at the origin we can cancel the request now so we don't have to wait for the timeout
			if !step.ExpectRequestToOrigin {
				cancel()
			}

			//Check for errors from the origin server
			err = <-originErrorChannel
			if err != nil {
				fmt.Printf("Scenario '%s' failed on step '%s', origin server received unexpected request from caching server: '%s'\n", scenario.Name, step.Name, err.Error())
				os.Exit(1)
			}

			//If we expected to get a request we execute the cancel now so in any case the context is cleaned up
			if step.ExpectRequestToOrigin {
				cancel()
			}

			//Check if the response the client got from the caching server is expected
			if err := step.CacheResponseChecker.ResponseExpected(response); err != nil {
				fmt.Printf("Scenario '%s' failed on step '%s', got unexpected response from caching server: '%s'\n", scenario.Name, step.Name, err.Error())
				os.Exit(1)
			}

			fmt.Println("Step success: ", step.Name)
		}

		fmt.Println("Scenario success: ", scenario.Name)
	}
}

type OriginServerHandler struct {
	ContentHandler http.Handler
	RequestChannel chan *http.Request
}

func (o *OriginServerHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	o.RequestChannel <- req
	o.ContentHandler.ServeHTTP(resp, req)
}

//A IntergrationTestScenario tests a specific caching scenario which can consists of multiple requests
//Every scenario starts with a fresh setup
type IntergrationTestScenario struct {
	//The name of the scenario
	Name string

	//The steps to be executed
	Steps []IntergrationTestScenarioStep

	//The cache controller under test
	Controller *sharedhttpcache.CacheController
}

//A IntergrationTestScenarioStep
type IntergrationTestScenarioStep struct {
	//The Name of the test step
	Name string

	//The client request which will be sent to the cache
	ClientRequest *http.Request

	//Returns true if we expect the cache to make a request to the origin server
	ExpectRequestToOrigin bool

	//Return the checker which will be used to check if the request the origin server received from the caching server is expected
	CacheRequestChecker CacheRequestChecker

	//Get the origin server http handler
	OriginHandler http.Handler

	//Get the checker which will be used to check if the response the client received from the caching server is expected
	CacheResponseChecker CacheResponseChecker
}

type CacheRequestChecker interface {
	RequestExpected(request *http.Request) error
}

type CacheRequestCheckerFunc func(request *http.Request) error

func (crc CacheRequestCheckerFunc) RequestExpected(request *http.Request) error {
	return crc(request)
}

type CacheResponseChecker interface {
	ResponseExpected(response *http.Response) error
}

type CacheResponseCheckerFunc func(response *http.Response) error

func (crc CacheResponseCheckerFunc) ResponseExpected(response *http.Response) error {
	return crc(response)
}
