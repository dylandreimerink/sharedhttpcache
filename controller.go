package caching

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

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

	var response *http.Response

	cacheKey := GetCacheKey(cacheConfig, forwardConfig, req)

	//Optimization: only if the method is safe and cacheable will it be in the cache
	// if if one of the two is false we can save the cache loopup and just forward the request
	if IsMethodSafe(cacheConfig, req.Method) && IsMethodCacheable(cacheConfig, req.Method) {

		cachedResponse, ttl, err := controller.FindInCache(cacheKey)
		if err != nil {
			//TODO make erroring optional, if the cache fails we may just want to forward the request instead of erroring

			controller.Logger.WithError(err).WithField("cache-key", cacheKey).Error("Error while attempting to find cache key in cache")

			http.Error(resp, "Error while attempting to find cached response", http.StatusInternalServerError)

			return
		}

		//If there is a cached response
		if cachedResponse != nil {

			//The original request is stripped from the response when it comes from the cache
			//So replace it
			cachedResponse.Request = req

			//If response is fresh and we don't have to revalidate because of a no-cache directive
			if ttl > 0 && !RequestOrResponseHasNoCache(cachedResponse) {

				err = WriteCachedResponse(resp, cachedResponse, ttl)
				if err != nil {
					controller.Logger.WithError(err).Error("Error while writing cached response to http client")
					panic(err)
				}

				return
			}

			//response is stale

			revalidationRequest := MakeRevalidationRequest(req, cachedResponse)

			//If no revalidation request can be made the cached response can't be used
			if revalidationRequest != nil {
				validationResponse, err := ProxyToOrigin(transport, forwardConfig, revalidationRequest)

				//If the origin server can't be reached or a error is returned
				if err != nil || validationResponse.StatusCode > 500 {
					//Can't reach origin server or it returned an error

					if cacheConfig.HTTPWarnings {
						//TODO add warning to stored response
					}

					//Check if we are allowed the serve the stale content
					if MayServeStaleResponse(cacheConfig, cachedResponse) {

						WriteCachedResponse(resp, cachedResponse, ttl)

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

							http.Error(resp, "Unable to reach origin server while revalidating cache", http.StatusBadGateway)
						} else {
							WriteHTTPResponse(resp, validationResponse)
						}
					}

					return
				}

				//If the response is not modified we can refresh the response
				if validationResponse.StatusCode == http.StatusNotModified {

					if cacheConfig.HTTPWarnings {
						//TODO remove warnings from stored response
					}

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
				//TODO invalidate cache key
			}
		}
	}

	//If response has not been set by the revalidation process
	if response == nil {
		response, err = ProxyToOrigin(transport, forwardConfig, req)
		if err != nil {

			//Log as a warning since errors here are exprected when a origin server is down
			controller.Logger.WithError(err).WithFields(logrus.Fields{
				"transport":      transport,
				"forward-config": forwardConfig,
				"request":        req,
			}).Warning("Error while proxying request to origin server")

			http.Error(resp, "Unable to contact origin server", http.StatusBadGateway)
			return
		}

		//TODO Deal with 101 Switching Protocols responses: (WebSocket, h2c, etc) https://golang.org/src/net/http/httputil/reverseproxy.go?s=3318:3379#L256
	}

	//If the response has no date the proxy must set it as per section 7.1.1.2 of RFC7231
	if response.Header.Get("Date") == "" {
		response.Header.Set("Date", time.Now().Format(http.TimeFormat))
	}

	//TODO invalidate cache entries, unsafe methods can invalidate other cache entries

	//TODO remove response hop-to-hop headers https://golang.org/src/net/http/httputil/reverseproxy.go?s=3318:3379#L264

	//If the response is cacheable
	if ShouldStoreResponse(cacheConfig, response) {

		//Get ttl and check if the response is not considered stale on arrival
		if ttl := GetResponseTTL(cacheConfig, response); ttl > 0 {

			err = controller.StoreResponseInCache(cacheKey, response, ttl)
			if err != nil {
				controller.Logger.WithError(err).WithFields(logrus.Fields{
					"cache-key": cacheKey,
					"response":  response,
				}).Error("Error while attempting to store response in cache")

				//TODO handle gracefully so the requests can continue even if we can't store the response
				panic(err)
			}

			response, _, err = controller.FindInCache(cacheKey)
			if err != nil {
				panic(err)
			}
		}
	}

	//TODO add warnings https://tools.ietf.org/html/rfc7234#section-5.5

	err = WriteHTTPResponse(resp, response)
	if err != nil {
		controller.Logger.WithError(err).Error("Error while writing response to http client")

		panic(err)
	}
}

func (controller *CacheController) StoreResponseInCache(cacheKey string, response *http.Response, ttl time.Duration) error {

	pipeReader, pipeWriter := io.Pipe()

	//Make a error reporting mechanism
	writeErrChan := make(chan error)

	//Write the response is a different goroutine because otherwise we risk a deadlock
	go func() {
		err := response.Write(pipeWriter)
		pipeWriter.Close()
		writeErrChan <- err
	}()

	storeErr := controller.StoreInCache(cacheKey, pipeReader, ttl)
	writeErr := <-writeErrChan

	if storeErr != nil {
		return storeErr
	}

	if writeErr != nil {
		return writeErr
	}

	return nil
}

func MayServeStaleResponse(cacheConfig *CacheConfig, response *http.Response) bool {
	//TODO check if allowed to serve stale response

	return false
}

func ProxyToOrigin(transport http.RoundTripper, forwardConfig *ForwardConfig, req *http.Request) (*http.Response, error) {
	//TODO be a proper reverse proxy https://golang.org/src/net/http/httputil/reverseproxy.go?s=3318:3379#L177
	// TODO Remove hop to hop headers
	// TODO set Forwarded-For and X-Forwarded-For header
	// TODO allow trailer

	if forwardConfig.TLS {
		req.URL.Scheme = "https"
	} else {
		req.URL.Scheme = "http"
	}

	req.URL.Host = forwardConfig.Host

	//Forward request to origin server
	return transport.RoundTrip(req)
}

func WriteHTTPResponse(rw http.ResponseWriter, response *http.Response) error {
	//Set all response headers in the response writer
	for key, values := range response.Header {
		rw.Header()[key] = values
	}

	rw.WriteHeader(response.StatusCode)

	//Close the body before returning
	defer response.Body.Close()
	_, err := io.Copy(rw, response.Body)

	return err
}

//WriteCachedResponse writes a cached response to a response writer
// this function should be used to write cached responses because it modifies the response to comply with the RFC's
func WriteCachedResponse(rw http.ResponseWriter, cachedResponse *http.Response, ttl time.Duration) error {

	age := -1

	dateString := cachedResponse.Header.Get("Date")
	if dateString != "" {
		date, err := http.ParseTime(dateString)
		if err == nil {

			//Get the second difference between date and now
			// this is the apparent_age method described in section 4.2.3 of RFC 7234
			age = int(time.Now().Sub(date).Seconds())
		}
	}

	//If the age is positive we add the header. Negative ages are not allowed
	if age >= 0 {
		cachedResponse.Header.Set("Age", strconv.Itoa(age))
	}

	return WriteHTTPResponse(rw, cachedResponse)
}

func (controller *CacheController) RefreshCacheEntry(cacheKey string, ttl time.Duration) error {

	for _, cacheLayer := range controller.Layers {
		err := cacheLayer.Refresh(cacheKey, ttl)
		if err != nil {
			//TODO detect expected vs unexpected errors

			controller.Logger.WithError(err).WithField("cache-key", cacheKey).Error("Error while refreshing cache entry")
		}
	}

	return nil
}

//StoreInCache attempts to store the entity in the cache
func (controller *CacheController) StoreInCache(cacheKey string, entry io.ReadCloser, ttl time.Duration) error {

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

//FindInCache attempts to find a cached response in the caching layers
// it returns the cached response and the TTL. A negative TTL means the response is stale
func (controller *CacheController) FindInCache(cacheKey string) (*http.Response, time.Duration, error) {

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

//GetCacheKey generates a cache key for the request according to the requirement in section 4 of RFC7234
// The acception being the secondary keys, they are not supported as of this moment.
func GetCacheKey(cacheConfig *CacheConfig, forwardConfig *ForwardConfig, req *http.Request) string {

	//TODO custom cache keys

	//TODO support secondary keys

	buf := &bytes.Buffer{}

	buf.WriteString(req.Method)
	buf.WriteString(GetEffectiveURI(req, forwardConfig))

	return buf.String()
}

//GetEffectiveURI returns the effective URI as string generated from a request object
// https://tools.ietf.org/html/rfc7230#section-5.5
func GetEffectiveURI(req *http.Request, forwardConfig *ForwardConfig) string {

	//If the request URI is in the absolute-form, just return it
	if req.URL.Host != "" && req.URL.Scheme != "" {
		return req.URL.String()
	}

	//Otherwise build the absolute URI ourselfs
	effectiveURI := &url.URL{}

	if req.TLS == nil {
		effectiveURI.Scheme = "http"
	} else {
		effectiveURI.Scheme = "https"
	}

	//If the host header is set in the request or in the URI this will be true
	if req.Host != "" {
		effectiveURI.Host = req.Host
	} else {
		effectiveURI.Host = forwardConfig.Host
	}

	//If request is in asterisk form we leave the path and query empty
	if req.URL.Path != "*" {
		effectiveURI.Path = req.URL.Path
		effectiveURI.RawPath = req.URL.RawPath

		//Parse and re-encode the query, this causes the query to be sorted by key
		// sort order is important when the effective uri is used in a cache key
		queryValues, err := url.ParseQuery(req.URL.RawQuery)
		if err == nil {
			effectiveURI.RawQuery = queryValues.Encode()
		}
	}

	return effectiveURI.String()
}

//A CacheConfigResolver resolves which cache config to use for which request.
// Different websites or even different pages on the same site can have different cache settings
type CacheConfigResolver interface {

	//GetCacheConfig is called to resolve a CacheConfig depending on the request
	// If nil is returned the default config will be used
	GetCacheConfig(req *http.Request) *CacheConfig
}

//A TransportResolver resolves which transport should be used for a particulair request
type TransportResolver interface {

	//GetTransport is called to resolve a CacheConfig depending on the request
	// If nil is returned the default transport will be used
	GetTransport(req *http.Request) http.RoundTripper
}

//The ForwardConfig holds information about how to forward traffic to the origin server
type ForwardConfig struct {
	//Can be a Hostname or a IP address and optionally the tcp port
	// if no port is specified the default http or https port is used based on the TLS variable
	Host string

	//If a https (http over TLS) connection should be used
	TLS bool
}

//A ForwardConfigResolver resolves which forward config should be used for a particulair request
type ForwardConfigResolver interface {

	//GetForwardConfig is called to resolve a ForwardConfig depending on the request
	// If nil is returned the default forwardConfig will be used
	GetForwardConfig(req *http.Request) *ForwardConfig
}
