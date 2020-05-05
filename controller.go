package sharedhttpcache

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/context"

	"github.com/dylandreimerink/sharedhttpcache/layer"

	"github.com/sirupsen/logrus"
)

//The CacheController is the high level interface for a cache. The cache controller calls the caching logic and
// handles storing and retrieving of cached responses.
type CacheController struct {

	//The default config is used if no host could be matched
	// if nil on first usage the default config CacheConfig from NewCacheConfig will be used
	DefaultCacheConfig *CacheConfig

	//CacheConfigResolver can optionally be set.
	// If not nil the CacheConfigResolver will be used to determine which cache config to use for a given request
	// If the CacheConfigResolver is not set the default config will always be used
	CacheConfigResolver CacheConfigResolver

	//The default transport used to contact the origin server
	// If nil the http.DefaultTransport will be used
	DefaultTransport http.RoundTripper

	//TransportResolver can optionally be set.
	// If not nil the TransportResolver will be used to determine which transport to use for a given request
	// If the TransportResolver is not set the default transport will always be used
	TransportResolver TransportResolver

	//The default config used to forward requests to the origin server
	// If nil all requests using the default config will return a 503 error
	DefaultForwardConfig *ForwardConfig

	//ForwardConfigResolver can optionally be set.
	// If not nil the ForwardConfigResolver will be used to determin which forwardConfig to use for a given request
	// If the ForwardConfigResolver is not set the DefaultForwardConfig will be used
	ForwardConfigResolver ForwardConfigResolver

	//The storage layers which will be searched, the layers are searched in order
	// Layers should be arranged from fastest to slowest
	// Faster caching layers typically have less capacity and thus will replace content sooner
	Layers []layer.CacheLayer

	//The Logger which will be used for logging
	// if nil the default logger will be used
	Logger *logrus.Logger
}

func (controller *CacheController) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	var err error

	if controller.Logger == nil {
		controller.Logger = logrus.New()
	}

	if controller.DefaultCacheConfig == nil {
		controller.DefaultCacheConfig = NewCacheConfig()
	}

	cacheConfig := controller.DefaultCacheConfig

	if controller.CacheConfigResolver != nil {
		if resolvedConfig := controller.CacheConfigResolver.GetCacheConfig(req); resolvedConfig != nil {
			cacheConfig = resolvedConfig
		}
	}

	forwardConfig := controller.DefaultForwardConfig

	if controller.ForwardConfigResolver != nil {
		if resolvedConfig := controller.ForwardConfigResolver.GetForwardConfig(req); resolvedConfig != nil {
			forwardConfig = resolvedConfig
		}
	}

	//Set default transport
	transport := controller.DefaultTransport

	//Use resolver to get transport based on request
	if controller.TransportResolver != nil {
		if transportConfig := controller.TransportResolver.GetTransport(req); transportConfig != nil {
			transport = transportConfig
		}
	}

	//If default is nil and resolver is nil or returned nil use http default transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	//TODO handle validation request from client, section 4.3.2 of RFC 7234

	primaryCacheKey := getPrimaryCacheKey(cacheConfig, forwardConfig, req)

	response, stop := controller.getCachedResponse(cacheConfig, forwardConfig, transport, resp, req, primaryCacheKey)
	if stop {
		return
	}

	// If response has not been set from the cache or by the revalidation process
	// Proxy the request to the origin server
	if response == nil {
		response, stop = controller.proxyRequestToOrigin(cacheConfig, forwardConfig, transport, resp, req)
		if stop {
			return
		}
	}

	//If the response has no date the proxy must set it as per section 7.1.1.2 of RFC7231
	if response.Header.Get(DateHeader) == "" {
		response.Header.Set(DateHeader, time.Now().Format(http.TimeFormat))
	}

	response = controller.storeResponse(cacheConfig, req, response, primaryCacheKey)

	//TODO add warnings https://tools.ietf.org/html/rfc7234#section-5.5

	err = writeHTTPResponse(resp, response)
	if err != nil {
		controller.Logger.WithError(err).Error("Error while writing response to http client")

		panic(err)
	}
}

func (controller *CacheController) proxyRequestToOrigin(
	cacheConfig *CacheConfig,
	forwardConfig *ForwardConfig,
	transport http.RoundTripper,
	resp http.ResponseWriter,
	req *http.Request,
) (
	*http.Response,
	bool,
) {

	//Create a forward context which will stop the connection to the backend if the connection from the clients stops
	ctx, cancel := context.WithCancel(req.Context())
	if cancel != nil {
		defer cancel()
	}

	response, err := proxyToOrigin(ctx, transport, forwardConfig, req)
	if err != nil {

		//Log as a warning since errors here are exprected when a origin server is down
		controller.Logger.WithError(err).WithFields(logrus.Fields{
			"transport":      transport,
			"forward-config": forwardConfig,
			"request":        req,
		}).Warning("Error while proxying request to origin server")

		http.Error(resp, "Unable to contact origin server", http.StatusBadGateway)

		return response, true
	}

	//If a request method is unsafe, invalidate cache, RFC 7234 section 4.4
	if !isMethodSafe(cacheConfig, req.Method) {

		//Only invalidate if the response is a 'non-error response'
		if response.StatusCode >= 200 && response.StatusCode < 400 {

			urls := []string{getEffectiveURI(req, forwardConfig)}

			locationVal := response.Header.Get("Location")
			if location, err := url.Parse(locationVal); err == nil {
				locationPseudoRequest := &http.Request{
					URL:  location,
					TLS:  req.TLS,
					Host: req.Host,
				}

				urls = append(urls, getEffectiveURI(locationPseudoRequest, forwardConfig))
			}

			contentLocationVal := response.Header.Get("Content-Location")
			if contentLocation, err := url.Parse(contentLocationVal); err == nil {
				contentLocationPseudoRequest := &http.Request{
					URL:  contentLocation,
					TLS:  req.TLS,
					Host: req.Host,
				}

				urls = append(urls, getEffectiveURI(contentLocationPseudoRequest, forwardConfig))
			}

			for _, url := range urls {
				for _, method := range cacheConfig.SafeMethods {
					//TODO use a method which also accounts for custom cache keys
					primaryKey := method + url

					secondaryKeys, _, err := controller.findSecondaryKeysInCache(primaryKey)
					if err != nil {
						controller.Logger.WithError(err).WithField("cache-key", primaryKey).Error("Error while attempting to find secondary cache key in cache")
					}

					if len(secondaryKeys) == 0 {
						secondaryKeys = []string{""}
					}

					for _, secondaryKey := range secondaryKeys {

						_, ttl, _ := controller.findResponseInCache(primaryKey + secondaryKey)
						if ttl >= 0 {

							//Set the ttl negative, so it will no longer be fresh
							err = controller.refreshCacheEntry(primaryKey+secondaryKey, time.Duration(-1))
							if err != nil {
								controller.Logger.WithError(err).WithField("cache-key", primaryKey+secondaryKey).Error("Error while attempting to set ttl of cache key to -1")
							}
						}
					}
				}
			}

		}
	}

	//TODO Deal with 101 Switching Protocols responses: (WebSocket, h2c, etc) https://golang.org/src/net/http/httputil/reverseproxy.go?s=3318:3379#L256

	return response, false
}

func (controller *CacheController) getCachedResponse(
	cacheConfig *CacheConfig,
	forwardConfig *ForwardConfig,
	transport http.RoundTripper,
	resp http.ResponseWriter,
	req *http.Request,
	primaryCacheKey string,
) (*http.Response, bool) {

	var response *http.Response

	//Optimization: only if the method is safe and cacheable will it be in the cache
	// if if one of the two is false we can save the cache loopup and just forward the request
	if isMethodSafe(cacheConfig, req.Method) && isMethodCacheable(cacheConfig, req.Method) {

		secondaryKeys, _, err := controller.findSecondaryKeysInCache(primaryCacheKey)
		if err != nil {
			controller.Logger.WithError(err).WithField("cache-key", primaryCacheKey).Error("Error while attempting to find secondary cache key in cache")
		}

		secondaryCacheKey := getSecondaryCacheKey(secondaryKeys, req)

		//The full cacheKey is the primary cache key plus the secondary cache key
		cacheKey := primaryCacheKey + secondaryCacheKey

		cachedResponse, ttl, err := controller.findResponseInCache(cacheKey)
		if err != nil {
			//TODO make erroring optional, if the cache fails we may just want to forward the request instead of erroring

			controller.Logger.WithError(err).WithField("cache-key", cacheKey).Error("Error while attempting to find cache key in cache")

			http.Error(resp, "Error while attempting to find cached response", http.StatusInternalServerError)

			return response, true
		}

		//If there is a cached response
		if cachedResponse != nil {

			//The original request is stripped from the response when it comes from the cache
			//So replace it
			cachedResponse.Request = req

			//The value of of the max-age header
			maxAge := int64(-1)

			//The value we will need to compare the cache entry ttl to
			compareTTL := int64(0)

			for _, directive := range splitCacheControlHeader(req.Header[CacheControlHeader]) {
				if strings.HasPrefix(directive, MaxAgeDirective) {
					//TODO check ofr quoted-string form
					maxAgeString := strings.TrimPrefix(directive, MaxAgeDirective+"=")
					maxAge, err = strconv.ParseInt(maxAgeString, 10, 0)
					if err != nil {
						//TODO make a warning log of the protocol violation
						maxAge = -1
					}
					continue
				}

				if strings.HasPrefix(directive, "max-stale") {
					//TODO check ofr quoted-string form
					maxStaleString := strings.TrimPrefix(directive, "max-stale=")
					maxStale, err := strconv.ParseInt(maxStaleString, 10, 0)

					compareTTL = -maxStale

					//If error no (valid) value is specified and thus any stale response is accepted
					if err != nil {
						compareTTL = math.MinInt64
					}
					continue
				}

				if strings.HasPrefix(directive, "min-fresh") {
					//TODO check ofr quoted-string form
					minFreshString := strings.TrimPrefix(directive, "min-fresh=")
					compareTTL, err = strconv.ParseInt(minFreshString, 10, 0)
					if err != nil {
						//TODO make a warning log of the protocol violation
						compareTTL = 0
					}
					continue
				}
			}

			clientWantsResponse := true

			//If compareTTL is negative the max-stale directive is set and if max-age is defined
			if compareTTL >= 0 && maxAge > -1 {

				//Get the age of the cached response
				responseAge := getResponseAge(cachedResponse)

				//if the cache is older than the client requested
				if responseAge > maxAge {
					clientWantsResponse = false
				}

			}

			cachedResponseIsFresh := ttl > (time.Duration(compareTTL) * time.Second)
			cachedResponseHasNoCache := requestOrResponseHasNoCache(cachedResponse)
			cachedresponseHasMustRevalidate := responseHasMustRevalidate(cachedResponse)

			if cachedResponseIsFresh && //If the response is older than the TTL it is stale
				!cachedResponseHasNoCache && //If the request or response contains a no-cache we can't return a cached result
				!cachedresponseHasMustRevalidate && //If the response contains a must-revalidate, we must always revalidate, can serve from cache
				clientWantsResponse { //If the client wants a response which is fresher than what we have, we can't serve the cached response

				err = writeCachedResponse(resp, cachedResponse, ttl)
				if err != nil {
					controller.Logger.WithError(err).Error("Error while writing cached response to http client")
					panic(err)
				}

				return response, true
			}

			//response is stale

			revalidationRequest := makeRevalidationRequest(req, cachedResponse)

			//If no revalidation request can be made the cached response can't be used
			if revalidationRequest != nil {

				//Create a forward context which will stop the connection to the backend if the connection from the clients stops
				ctx, cancel := context.WithCancel(req.Context())
				if cancel != nil {
					defer cancel()
				}

				validationResponse, err := proxyToOrigin(ctx, transport, forwardConfig, revalidationRequest)

				//If the origin server can't be reached or a error is returned
				if err != nil || validationResponse.StatusCode > 500 {
					//Can't reach origin server or it returned an error

					// if cacheConfig.HTTPWarnings {
					//TODO add warning to stored response
					// }

					//Check if we are allowed the serve the stale content
					if mayServeStaleResponse(cacheConfig, cachedResponse) {

						//If the response contains a no-cache directive with a field-list strip the headers from the response
						//Section 5.2.2.2 of RFC 7234
						for _, directive := range splitCacheControlHeader(response.Header[CacheControlHeader]) {
							if strings.HasPrefix(directive, NoCacheDirective+"=") {
								fieldList := strings.TrimPrefix(directive, NoCacheDirective+"=")
								fieldList = strings.Trim(fieldList, "\"")
								for _, fieldName := range strings.Split(fieldList, ",") {
									cachedResponse.Header.Del(strings.TrimSpace(fieldName))
								}
							}
						}

						err := writeCachedResponse(resp, cachedResponse, ttl)
						if err != nil {
							controller.Logger.WithError(err).Error("Error while writing stale response to client")
						}

					} else {

						if validationResponse == nil {
							log := logrus.NewEntry(controller.Logger)
							if err != nil {
								log = log.WithError(err)
							}

							log = log.WithFields(logrus.Fields{
								"transport":      transport,
								"forward-config": forwardConfig,
								"request":        revalidationRequest,
								"response":       validationResponse,
							})

							log.Warning("Unable to revalidate cache at origin server")

							//Send a 504 since we can't revalidate and we are not allowed to serve the content stale
							//504 is used since it is mentioned in section 5.2.2.1 of RFC7234
							http.Error(resp, "Unable to reach origin server while revalidating cache", http.StatusGatewayTimeout)
						} else {
							//If we reached this block it means we were able to contact the origin but it returned a 5xx code and are not allowed to serve a stale response
							//So we have to send the error to the client as per section 4.3.3 of RFC7234
							err := writeHTTPResponse(resp, validationResponse)
							if err != nil {
								controller.Logger.WithError(err).Error("Error while writing validation response to client")
							}
						}
					}

					return response, true
				}

				//If the response is not modified we can refresh the response
				if validationResponse.StatusCode == http.StatusNotModified {

					// if cacheConfig.HTTPWarnings {
					//TODO remove warnings from stored response
					// }

					//Overwrite cached headers with the headers from the validation response
					for header, value := range validationResponse.Header {
						cachedResponse.Header[header] = value
					}

					//Set the updated cachedResponse as the response
					// this will cause the ttl to be recalculated and the updated cachedResponse to be set as new value for the cache key
					response = cachedResponse

					//If status code is 200 we can use this response
				} else if validationResponse.StatusCode == http.StatusOK {

					//Set validation response as the response to be cached and send to the client
					response = validationResponse
				}

				//TODO if 206 save partial response if partial responses are allowed

				//If no revalidation can be done or precondition failed
			} else {
				// A validation request could not be made for the stored response
				// Most likely because there is no Last-Modified or Etag header in the response

				//In the case the only reason to revalidate was to validate fields in the no-cache CC directive
				if cachedResponseIsFresh && //If the response is older than the TTL it is stale
					!cachedresponseHasMustRevalidate && //If the response contains a must-revalidate, we must always revalidate, can serve from cache
					clientWantsResponse { //If the client wants a response which is fresher than what we have, we can't serve the cached response

					noCacheFields := false

					//If the response contains a no-cache directive with a field-list strip the headers from the response
					//Section 5.2.2.2 of RFC 7234
					for _, directive := range splitCacheControlHeader(cachedResponse.Header[CacheControlHeader]) {
						if strings.HasPrefix(directive, NoCacheDirective+"=") {
							noCacheFields = true

							fieldList := strings.TrimPrefix(directive, NoCacheDirective+"=")
							fieldList = strings.Trim(fieldList, "\"")
							for _, fieldName := range strings.Split(fieldList, ",") {
								cachedResponse.Header.Del(strings.TrimSpace(fieldName))
							}
						}
					}

					//If the Cache-Control header contained a no-cache directive with a field set
					// We can may return the cached response without the headers in the fieldset
					if noCacheFields {
						err := writeCachedResponse(resp, cachedResponse, ttl)
						if err != nil {
							controller.Logger.WithError(err).Error("Error while writing un-revalidated response to client")
						}

						return response, true
					}
				}

				//TODO invalidate cache key
			}
		}
	}

	return response, false
}

//storeResponse stores the response if it should be stored
func (controller *CacheController) storeResponse(cacheConfig *CacheConfig, req *http.Request, response *http.Response, primaryCacheKey string) *http.Response {

	//If the response is cacheable
	if shouldStoreResponse(cacheConfig, response) {

		//Get ttl and check if the response is not considered stale on arrival
		if ttl := getResponseTTL(cacheConfig, response); ttl > 0 {

			//Get the secondary key fields from the response (if any exist)
			secondaryKeyFields := []string{}
			vary := response.Header.Get(VaryHeader)
			if vary != "" {
				for _, key := range strings.Split(vary, ",") {
					secondaryKeyFields = append(secondaryKeyFields, strings.TrimSpace(key))
				}
			}

			//Get the secondaryCacheKey
			secondaryCacheKey := getSecondaryCacheKey(secondaryKeyFields, req)

			//Append the two to get the full cache key
			cacheKey := primaryCacheKey + secondaryCacheKey

			//Store the latest set of secondary keys we find
			//this can cause issues if the origin returns a different value in Vary for different primary cache keys
			//TODO look into this
			err := controller.storeSecondaryKeysInCache(primaryCacheKey, secondaryKeyFields, ttl)
			if err != nil {

				controller.Logger.WithError(err).WithFields(logrus.Fields{
					"cache-key": cacheKey,
					"response":  response,
				}).Error("Error while attempting to store secondary cache keys in cache")

				//TODO handle gracefully so the requests can continue even if we can't store the response
				panic(err)
			}

			err = controller.storeResponseInCache(cacheKey, response, ttl)
			if err != nil {
				controller.Logger.WithError(err).WithFields(logrus.Fields{
					"cache-key": cacheKey,
					"response":  response,
				}).Error("Error while attempting to store response in cache")

				//TODO handle gracefully so the requests can continue even if we can't store the response
				panic(err)
			}

			response, _, err = controller.findResponseInCache(cacheKey)
			if err != nil {
				panic(err)
			}
		}
	}

	return response
}

//storeResponseInCache stores the given response in the cache under the cacheKey
//The main difference with storeInCache is that this function handels the generation of the byte representation of the response
func (controller *CacheController) storeResponseInCache(cacheKey string, response *http.Response, ttl time.Duration) error {

	pipeReader, pipeWriter := io.Pipe()

	//Make a error reporting mechanism
	writeErrChan := make(chan error)

	//Write the response is a different goroutine because otherwise we risk a deadlock
	go func() {
		err := response.Write(pipeWriter)
		pipeWriter.Close()
		writeErrChan <- err
	}()

	storeErr := controller.storeInCache(cacheKey, pipeReader, ttl)
	writeErr := <-writeErrChan

	if storeErr != nil {
		return fmt.Errorf("Store error: %w", storeErr)
	}

	if writeErr != nil {
		return fmt.Errorf("Write error: %w", writeErr)
	}

	return nil
}

//storeSecondaryKeysInCache creates a special purpose cache entry which stores a list of header names used as secondary cache keys
func (controller *CacheController) storeSecondaryKeysInCache(primaryCacheKey string, keys []string, ttl time.Duration) error {

	secondaryCacheKeys := "secondary-keys" + primaryCacheKey

	sort.Strings(keys)

	keysString := strings.Join(keys, "\n")

	keysReader := ioutil.NopCloser(strings.NewReader(keysString))

	return controller.storeInCache(secondaryCacheKeys, keysReader, ttl)
}

//refreshCacheEntry updates the ttl of the given cacheKey
func (controller *CacheController) refreshCacheEntry(cacheKey string, ttl time.Duration) error {

	for _, cacheLayer := range controller.Layers {
		err := cacheLayer.Refresh(cacheKey, ttl)
		if err != nil {
			//TODO detect expected vs unexpected errors

			controller.Logger.WithError(err).WithField("cache-key", cacheKey).Error("Error while refreshing cache entry")
		}
	}

	return nil
}

//storeInCache attempts to store the entity in the cache
func (controller *CacheController) storeInCache(cacheKey string, entry io.ReadCloser, ttl time.Duration) error {

	//Make sure the entry is always closed so we don't leak resources
	defer entry.Close()

	//TODO make a good fail mechanism. Currently if the first layer errors when trying to write to the cache
	// the content is lost. This is likely to happen if the first layer has finite capacity.
	// If the first layer only has 512 MB and a 1G movie is cached we have a issue

	//Loop over all layers and insert the cached entity
	for _, cacheLayer := range controller.Layers {

		err := cacheLayer.Set(cacheKey, entry, ttl)
		if err != nil {
			return err
		}

		//TODO asynchronous writes. After the first layer has been successfully written writing to the next layers can happen asynchronously
		// this way the latency of the initial request is improved

		//Replace the entity with a reader from the previous layer
		// We have to do this because the initial reader has now been fully read and closed
		entry, _, err = cacheLayer.Get(cacheKey)
		if err != nil {
			return err
		}
	}

	return nil
}

//findResponseInCache attempts to find a cached response in the caching layers
// it returns the cached response and the TTL. A negative TTL means the response is stale
func (controller *CacheController) findResponseInCache(cacheKey string) (*http.Response, time.Duration, error) {

	//TODO if a entry is found in a lower layer consider moving it to a higher layer if it is requested more frequently

	for _, cacheLayer := range controller.Layers {
		reader, ttl, err := cacheLayer.Get(cacheKey)
		if err != nil {
			return nil, -1, err
		}

		//If the entry was not found
		if reader == nil {
			continue
		}

		httpReader := bufio.NewReader(reader)

		//Close the cache reader when we are done
		defer reader.Close()

		response, err := http.ReadResponse(httpReader, nil)
		if err != nil {
			return nil, -1, err
		}

		return response, ttl, nil
	}

	//If entry wasn't found in any layer
	return nil, -1, nil
}

//findSecondaryKeysInCache attempts to find the secondary keys defined for a set of responses with the given primary cache key
//It does this by prepending "secondary-keys" to the cache key and splitting the result on newlines
//
//If no secondary keys exist or can't be found in the cache a slice of zero length will be returned
//The ttl will be -1 of no entry was found
func (controller *CacheController) findSecondaryKeysInCache(cacheKey string) ([]string, time.Duration, error) {

	//TODO if a entry is found in a lower layer consider moving it to a higher layer if it is requested more frequently

	secondaryCacheKey := "secondary-keys" + cacheKey

	for _, cacheLayer := range controller.Layers {
		reader, ttl, err := cacheLayer.Get(secondaryCacheKey)
		if err != nil {
			return []string{}, -1, err
		}

		//If the entry was not found
		if reader == nil {
			continue
		}

		keyReader := bufio.NewReader(reader)

		//Close the cache reader when we are done
		defer reader.Close()

		keys := []string{}

		scanner := bufio.NewScanner(keyReader)
		for scanner.Scan() {
			keys = append(keys, scanner.Text())
		}

		return keys, ttl, scanner.Err()
	}

	//If entry wasn't found in any layer
	return []string{}, -1, nil
}
