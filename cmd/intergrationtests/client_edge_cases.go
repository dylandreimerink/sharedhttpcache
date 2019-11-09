package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/dylandreimerink/sharedhttpcache"
	"github.com/dylandreimerink/sharedhttpcache/layer"
)

func init() {
	Scenarios = append(Scenarios,
		clientCacheRequirements(),
	)
}

//Test the cache respects the max-age, max-stale and min-fresh directives
func clientCacheRequirements() IntergrationTestScenario {

	initialRequest := &http.Request{
		Method: http.MethodGet,
		URL:    URLMustParse("/lorum-ipsum"),
	}

	badCaseOriginResponse := http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		_, err := resp.Write([]byte("Not the same content"))
		if err != nil {
			fmt.Printf("Error while writing origin response: %s", err.Error())
		}
	})

	badCaseResponseChecker := CacheResponseCheckerFunc(func(resp *http.Response) error {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if string(body) == "Not the same content" {
			return errors.New("Non cached response returned")
		}

		return nil
	})

	badCaseResponseCheckerWithDelay := CacheResponseCheckerFunc(func(resp *http.Response) error {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if string(body) == "Not the same content" {
			return errors.New("Non cached response returned")
		}

		//Give the cache entry time to age for the next step
		time.Sleep(1100 * time.Millisecond)

		return nil
	})

	return IntergrationTestScenario{
		Name: "Respect client max-age, max-stale and min-fresh directives",
		Controller: &sharedhttpcache.CacheController{
			Layers: []layer.CacheLayer{
				layer.NewInMemoryCacheLayer(64 * 1024 * 1024),
			},
		},
		Steps: []IntergrationTestScenarioStep{
			IntergrationTestScenarioStep{
				Name:          "First request - make cache",
				ClientRequest: initialRequest,
				OriginHandler: http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {

					resp.Header().Add("Cache-Control", "max-age=120")
					resp.Header().Add("Date", time.Now().UTC().Format(http.TimeFormat))

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
				Name:                  "Second request - confirm entry is cached",
				ClientRequest:         initialRequest,
				OriginHandler:         badCaseOriginResponse,
				ExpectRequestToOrigin: false,
				CacheResponseChecker:  badCaseResponseChecker,
			},
			IntergrationTestScenarioStep{
				Name: "Third request - min-fresh < 120",
				ClientRequest: &http.Request{
					Method: http.MethodGet,
					URL:    URLMustParse("/lorum-ipsum"),
					Header: http.Header{
						"Cache-Control": []string{"min-fresh=110"},
					},
				},
				OriginHandler:         badCaseOriginResponse,
				ExpectRequestToOrigin: false,
				CacheResponseChecker:  badCaseResponseChecker,
			},
			IntergrationTestScenarioStep{
				Name: "Forth request - min-fresh > 120",
				ClientRequest: &http.Request{
					Method: http.MethodGet,
					URL:    URLMustParse("/lorum-ipsum"),
					Header: http.Header{
						"Cache-Control": []string{"min-fresh=130"},
					},
				},
				OriginHandler: http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
					resp.Header().Add("Cache-Control", "max-age=120")
					resp.Header().Add("Date", time.Now().UTC().Format(http.TimeFormat))

					_, err := resp.Write([]byte("<html><head><title>Most basic updated page ever</title></head></html>"))
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

					if string(body) != "<html><head><title>Most basic updated page ever</title></head></html>" {
						return errors.New("The cached response was returned, the caching server didn't respect the min-fresh header")
					}

					return nil
				}),
			},
			IntergrationTestScenarioStep{
				Name: "Fifth request - max-age = 10",
				ClientRequest: &http.Request{
					Method: http.MethodGet,
					URL:    URLMustParse("/lorum-ipsum"),
					Header: http.Header{
						"Cache-Control": []string{"max-age=10"},
					},
				},
				OriginHandler:         badCaseOriginResponse,
				ExpectRequestToOrigin: false,
				CacheResponseChecker:  badCaseResponseCheckerWithDelay,
			},
			IntergrationTestScenarioStep{
				Name: "Sixth request - max-age = 0",
				ClientRequest: &http.Request{
					Method: http.MethodGet,
					URL:    URLMustParse("/lorum-ipsum"),
					Header: http.Header{
						"Cache-Control": []string{"max-age=0"},
					},
				},
				OriginHandler: http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
					resp.Header().Add("Cache-Control", "max-age=1")
					resp.Header().Add("Date", time.Now().UTC().Format(http.TimeFormat))

					_, err := resp.Write([]byte("<html><head><title>Most basic twice updated page ever</title></head></html>"))
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

					if string(body) != "<html><head><title>Most basic twice updated page ever</title></head></html>" {
						return errors.New("The cached response was returned, the caching server didn't respect the max-age header")
					}

					//Give the cache entry time to age for the next step
					time.Sleep(1 * time.Second)

					return nil
				}),
			},
			IntergrationTestScenarioStep{
				Name: "Seventh request - max-stale = 5",
				ClientRequest: &http.Request{
					Method: http.MethodGet,
					URL:    URLMustParse("/lorum-ipsum"),
					Header: http.Header{
						"Cache-Control": []string{"max-stale=5"},
					},
				},
				OriginHandler:         badCaseOriginResponse,
				ExpectRequestToOrigin: false,
				CacheResponseChecker:  badCaseResponseCheckerWithDelay,
			},
			IntergrationTestScenarioStep{
				Name: "Eighth request - max-stale = 1",
				ClientRequest: &http.Request{
					Method: http.MethodGet,
					URL:    URLMustParse("/lorum-ipsum"),
					Header: http.Header{
						"Cache-Control": []string{"max-stale=1"},
					},
				},
				OriginHandler: http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
					resp.Header().Add("Cache-Control", "max-age=1")
					resp.Header().Add("Date", time.Now().UTC().Format(http.TimeFormat))

					_, err := resp.Write([]byte("<html><head><title>Most basic thrice updated page ever</title></head></html>"))
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

					if string(body) != "<html><head><title>Most basic thrice updated page ever</title></head></html>" {
						return errors.New("The cached response was returned, the caching server didn't respect the max-stale header")
					}

					return nil
				}),
			},
		},
	}
}
