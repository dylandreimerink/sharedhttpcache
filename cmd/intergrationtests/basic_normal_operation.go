package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/dylandreimerink/sharedhttpcache/layer"

	"github.com/dylandreimerink/sharedhttpcache"
)

func init() {
	Scenarios = append(Scenarios,
		firstRequestTest(),
		htmlNotCachedByDefault(),
	)
}

func URLMustParse(urlString string) *url.URL {
	url, err := url.Parse(urlString)
	if err != nil {
		panic(err)
	}
	return url
}

//firstRequestTest is a basic test which confirms that the first request is proxied to the origin server
func firstRequestTest() IntergrationTestScenario {
	return IntergrationTestScenario{
		Name: "Proxy on first request",
		Controller: &sharedhttpcache.CacheController{
			Layers: []layer.CacheLayer{
				layer.NewInMemoryCacheLayer(64 * 1024 * 1024),
			},
		},
		Steps: []IntergrationTestScenarioStep{
			IntergrationTestScenarioStep{
				Name: "Second request",
				ClientRequest: &http.Request{
					Method: http.MethodGet,
					URL:    URLMustParse("/lorum-ipsum"),
				},
				CacheRequestChecker: CacheRequestCheckerFunc(func(req *http.Request) error {
					return nil
				}),
				OriginHandler: http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
					_, err := resp.Write([]byte("Lorem ipsum dolor sit amet, consectetur adipiscing elit"))
					if err != nil {
						fmt.Printf("Error while writing origin response: %s", err.Error())
					}
				}),
				ExpectRequestToOrigin: true,
				CacheResponseChecker: CacheResponseCheckerFunc(func(resp *http.Response) error {
					body, err := ioutil.ReadAll(resp.Body)
					if err != nil {
						return err
					}

					if string(body) != "Lorem ipsum dolor sit amet, consectetur adipiscing elit" {
						return errors.New("Expected response to be equal to the body sent by the origin server")
					}

					return nil
				}),
			},
		},
	}
}

//Test that html is not cached if the origin sends no cache directives
func htmlNotCachedByDefault() IntergrationTestScenario {

	request := &http.Request{
		Method: http.MethodGet,
		URL:    URLMustParse("/lorum-ipsum"),
	}

	return IntergrationTestScenario{
		Name: "Don't cache html by default",
		Controller: &sharedhttpcache.CacheController{
			Layers: []layer.CacheLayer{
				layer.NewInMemoryCacheLayer(64 * 1024 * 1024),
			},
		},
		Steps: []IntergrationTestScenarioStep{
			IntergrationTestScenarioStep{
				Name:          "First request",
				ClientRequest: request,
				CacheRequestChecker: CacheRequestCheckerFunc(func(req *http.Request) error {
					return nil
				}),
				OriginHandler: http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
					_, err := resp.Write([]byte("<html><head><title>Most basic page ever</title></head></html>"))
					if err != nil {
						fmt.Printf("Error while writing origin response: %s", err.Error())
					}
				}),
				ExpectRequestToOrigin: true,
				CacheResponseChecker: CacheResponseCheckerFunc(func(resp *http.Response) error {
					return nil
				}),
			},
			IntergrationTestScenarioStep{
				Name:          "Second request",
				ClientRequest: request,
				CacheRequestChecker: CacheRequestCheckerFunc(func(req *http.Request) error {
					return nil
				}),
				OriginHandler: http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
					_, err := resp.Write([]byte("Not the same content"))
					if err != nil {
						fmt.Printf("Error while writing origin response: %s", err.Error())
					}
				}),
				ExpectRequestToOrigin: true,
				CacheResponseChecker: CacheResponseCheckerFunc(func(resp *http.Response) error {
					body, err := ioutil.ReadAll(resp.Body)
					if err != nil {
						return err
					}

					if string(body) != "Not the same content" {
						return errors.New("Got cached response, expected to get fresh response from origin server")
					}

					return nil
				}),
			},
		},
	}
}
