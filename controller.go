package sharedhttpcache

import (
	"bufio"
	"bytes"
	"io"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"net/textproto"
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

	var response *http.Response

	primaryCacheKey := GetPrimaryCacheKey(cacheConfig, forwardConfig, req)

	//Optimization: only if the method is safe and cacheable will it be in the cache
	// if if one of the two is false we can save the cache loopup and just forward the request
	if IsMethodSafe(cacheConfig, req.Method) && IsMethodCacheable(cacheConfig, req.Method) {

		secondaryKeys, _, err := controller.FindSecondaryKeysInCache(primaryCacheKey)
		if err != nil {
			controller.Logger.WithError(err).WithField("cache-key", primaryCacheKey).Error("Error while attempting to find secondary cache key in cache")
		}

		secondaryCacheKey := GetSecondaryCacheKey(secondaryKeys, req)

		//The full cacheKey is the primary cache key plus the secondary cache key
		cacheKey := primaryCacheKey + secondaryCacheKey

		cachedResponse, ttl, err := controller.FindResponseInCache(cacheKey)
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

			//The value of of the max-age header
			maxAge := int64(-1)

			//The value we will need to compare the cache entry ttl to
			compareTTL := int64(0)

			for _, directive := range SplitCacheControlHeader(req.Header.Get(CacheControlHeader)) {
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

			//If response is fresh and we don't have to revalidate because of a no-cache directive
			if ttl > (time.Duration(compareTTL)*time.Second) && !RequestOrResponseHasNoCache(cachedResponse) && clientWantsResponse {

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

				//Create a forward context which will stop the connection to the backend if the connection from the clients stops
				ctx, cancel := createForwardContext(resp, req)
				if cancel != nil {
					defer cancel()
				}

				validationResponse, err := ProxyToOrigin(ctx, transport, forwardConfig, revalidationRequest)

				//If the origin server can't be reached or a error is returned
				if err != nil || validationResponse.StatusCode > 500 {
					//Can't reach origin server or it returned an error

					if cacheConfig.HTTPWarnings {
						//TODO add warning to stored response
					}

					//Check if we are allowed the serve the stale content
					if MayServeStaleResponse(cacheConfig, cachedResponse) {

						err := WriteCachedResponse(resp, cachedResponse, ttl)
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
							err := WriteHTTPResponse(resp, validationResponse)
							if err != nil {
								controller.Logger.WithError(err).Error("Error while writing validation response to client")
							}
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
		//Create a forward context which will stop the connection to the backend if the connection from the clients stops
		ctx, cancel := createForwardContext(resp, req)
		if cancel != nil {
			defer cancel()
		}

		response, err = ProxyToOrigin(ctx, transport, forwardConfig, req)
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
	if response.Header.Get(DateHeader) == "" {
		response.Header.Set(DateHeader, time.Now().Format(http.TimeFormat))
	}

	//TODO invalidate cache entries, unsafe methods can invalidate other cache entries

	//If the response is cacheable
	if ShouldStoreResponse(cacheConfig, response) {

		//Get ttl and check if the response is not considered stale on arrival
		if ttl := GetResponseTTL(cacheConfig, response); ttl > 0 {

			//Get the secondary key fields from the response (if any exist)
			secondaryKeyFields := []string{}
			vary := response.Header.Get(VaryHeader)
			if vary != "" {
				for _, key := range strings.Split(vary, ",") {
					secondaryKeyFields = append(secondaryKeyFields, strings.TrimSpace(key))
				}
			}

			//Get the secondaryCacheKey
			secondaryCacheKey := GetSecondaryCacheKey(secondaryKeyFields, req)

			//Append the two to get the full cache key
			cacheKey := primaryCacheKey + secondaryCacheKey

			//Store the latest set of secondary keys we find
			//this can cause issues if the origin returns a different value in Vary for different primary cache keys
			//TODO look into this
			err := controller.StoreSecondaryKeysInCache(primaryCacheKey, secondaryKeyFields, ttl)
			if err != nil {

				controller.Logger.WithError(err).WithFields(logrus.Fields{
					"cache-key": cacheKey,
					"response":  response,
				}).Error("Error while attempting to store secondary cache keys in cache")

				//TODO handle gracefully so the requests can continue even if we can't store the response
				panic(err)
			}

			err = controller.StoreResponseInCache(cacheKey, response, ttl)
			if err != nil {
				controller.Logger.WithError(err).WithFields(logrus.Fields{
					"cache-key": cacheKey,
					"response":  response,
				}).Error("Error while attempting to store response in cache")

				//TODO handle gracefully so the requests can continue even if we can't store the response
				panic(err)
			}

			response, _, err = controller.FindResponseInCache(cacheKey)
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

//createForwardContext creates a context which should be used when forwarding a request to a backend
func createForwardContext(resp http.ResponseWriter, req *http.Request) (context.Context, context.CancelFunc) {
	ctx := req.Context()
	var cancel context.CancelFunc

	if cn, ok := resp.(http.CloseNotifier); ok {
		ctx, cancel = context.WithCancel(ctx)
		notifyChan := cn.CloseNotify()
		go func() {
			select {
			case <-notifyChan:
				cancel()
			case <-ctx.Done():
			}
		}()
	}

	return ctx, cancel
}

//StoreResponseInCache stores the given response in the cache under the cacheKey
//The main difference with StoreInCache is that this function handels the generation of the byte representation of the response
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

//StoreSecondaryKeysInCache creates a special purpose cache entry which stores a list of header names used as secondary cache keys
func (controller *CacheController) StoreSecondaryKeysInCache(primaryCacheKey string, keys []string, ttl time.Duration) error {

	secondaryCacheKeys := "secondary-keys" + primaryCacheKey

	sort.Strings(keys)

	keysString := strings.Join(keys, "\n")

	keysReader := ioutil.NopCloser(strings.NewReader(keysString))

	return controller.StoreInCache(secondaryCacheKeys, keysReader, ttl)
}

//MayServeStaleResponse checks if according to the config and rules specified in RFC7234 the caching server is allowed to serve the response if it is stale
func MayServeStaleResponse(cacheConfig *CacheConfig, response *http.Response) bool {

	//If serving of stale responses is turned off
	if !cacheConfig.ServeStaleOnError {
		return false
	}

	if MayServeStaleResponseByExtension(cacheConfig, response) {
		return true
	}

	directives := SplitCacheControlHeader(response.Header.Get(CacheControlHeader))
	for _, directive := range directives {

		//If response contains a cache directive that disallowes stale responses section 4.2.4 of RFC7234
		if directive == MustRevalidateDirective || directive == ProxyRevalidateDirective ||
			directive == NoCacheDirective || strings.HasPrefix(directive, SMaxAgeDirective) {

			return false
		}
	}

	return true
}

//MayServeStaleResponseByExtension checks if there are any Cache-Control extensions which allow stale responses to be served
func MayServeStaleResponseByExtension(cacheConfig *CacheConfig, response *http.Response) bool {

	//TODO implement https://tools.ietf.org/html/rfc5861

	return false
}

// From net/http/httputil/reverseproxy.go
// removeConnectionHeaders removes hop-by-hop headers listed in the "Connection" header of h.
// See RFC 7230, section 6.1
func removeConnectionHeaders(h http.Header) {
	for _, f := range h["Connection"] {
		for _, sf := range strings.Split(f, ",") {
			if sf = strings.TrimSpace(sf); sf != "" {
				h.Del(sf)
			}
		}
	}
}

// From net/http/httputil/reverseproxy.go
// Hop-by-hop headers. These are removed when sent to the backend.
// As of RFC 7230, hop-by-hop headers are required to appear in the
// Connection header field. These are the headers defined by the
// obsoleted RFC 2616 (section 13.5.1) and are used for backward
// compatibility.
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection", // non-standard but still sent by libcurl and rejected by e.g. google
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",      // canonicalized version of "TE"
	"Trailer", // not Trailers per URL above; https://www.rfc-editor.org/errata_search.php?eid=4522
	"Transfer-Encoding",
	"Upgrade",
}

//ProxyToOrigin proxies a request to a origin server using the given config and return the response
func ProxyToOrigin(forwardContext context.Context, transport http.RoundTripper, forwardConfig *ForwardConfig, req *http.Request) (*http.Response, error) {
	//TODO add websocket support

	//Clone the request
	outreq := req.Clone(forwardContext)
	if req.ContentLength == 0 {
		outreq.Body = nil // Issue 16036: nil Body for http.Transport retries
	}
	if outreq.Header == nil {
		outreq.Header = make(http.Header) // Issue 33142: historical behavior was to always allocate
	}

	outreq.Close = false

	// TODO uncomment when adding websocket support
	// reqUpType := ""
	// if httpguts.HeaderValuesContainsToken(outreq.Header["Connection"], "Upgrade") {
	// 	reqUpType = strings.ToLower(outreq.Header.Get("Upgrade"))
	// }

	removeConnectionHeaders(outreq.Header)

	// Remove hop-by-hop headers to the backend. Especially
	// important is "Connection" because we want a persistent
	// connection, regardless of what the client sent to us.
	for _, h := range hopHeaders {
		hv := outreq.Header.Get(h)
		if hv == "" {
			continue
		}
		if h == "Te" && hv == "trailers" {
			// Issue 21096: tell backend applications that
			// care about trailer support that we support
			// trailers. (We do, but we don't go out of
			// our way to advertise that unless the
			// incoming client request thought it was
			// worth mentioning)
			continue
		}
		outreq.Header.Del(h)
	}

	// TODO uncomment when adding websocket support
	// // After stripping all the hop-by-hop connection headers above, add back any
	// // necessary for protocol upgrades, such as for websockets.
	// if reqUpType != "" {
	// 	outreq.Header.Set("Connection", "Upgrade")
	// 	outreq.Header.Set("Upgrade", reqUpType)
	// }

	if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		// If we aren't the first proxy retain prior
		// X-Forwarded-For information as a comma+space
		// separated list and fold multiple headers into one.
		if prior, ok := outreq.Header["X-Forwarded-For"]; ok {
			clientIP = strings.Join(prior, ", ") + ", " + clientIP
		}
		outreq.Header.Set("X-Forwarded-For", clientIP)
	}

	//Change the protocol of the url to the protocol specified in the forward config
	if forwardConfig.TLS {
		outreq.URL.Scheme = "https"
	} else {
		outreq.URL.Scheme = "http"
	}

	//Change the host in the url
	outreq.URL.Host = forwardConfig.Host

	//Forward request to origin server
	response, err := transport.RoundTrip(outreq)
	if err != nil {
		return nil, err
	}

	removeConnectionHeaders(response.Header)

	for _, h := range hopHeaders {
		response.Header.Del(h)
	}

	return response, nil
}

//WriteHTTPResponse writes a response the response writer
func WriteHTTPResponse(rw http.ResponseWriter, response *http.Response) error {

	//TODO add support for Trailers https://golang.org/src/net/http/httputil/reverseproxy.go?s=3318:3379#L276

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

func getResponseAge(response *http.Response) int64 {
	age := int64(-1)

	dateString := response.Header.Get(DateHeader)
	if dateString != "" {
		date, err := http.ParseTime(dateString)
		if err == nil {

			//Get the second difference between date and now
			// this is the apparent_age method described in section 4.2.3 of RFC 7234
			age = int64(time.Since(date).Seconds())
		}
	}

	//The age of a response can't be in the future
	if age < -1 {
		age = -1
	}

	return age
}

//WriteCachedResponse writes a cached response to a response writer
// this function should be used to write cached responses because it modifies the response to comply with the RFC's
func WriteCachedResponse(rw http.ResponseWriter, cachedResponse *http.Response, ttl time.Duration) error {

	age := getResponseAge(cachedResponse)

	//If the age is positive we add the header. Negative ages are not allowed
	if age >= 0 {
		cachedResponse.Header.Set("Age", strconv.FormatInt(age, 10))
	}

	return WriteHTTPResponse(rw, cachedResponse)
}

//RefreshCacheEntry updates the ttl of the given cacheKey
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

//FindResponseInCache attempts to find a cached response in the caching layers
// it returns the cached response and the TTL. A negative TTL means the response is stale
func (controller *CacheController) FindResponseInCache(cacheKey string) (*http.Response, time.Duration, error) {

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

//FindSecondaryKeysInCache attempts to find the secondary keys defined for a set of responses with the given primary cache key
//It does this by prepending "secondary-keys" to the cache key and splitting the result on newlines
//
//If no secondary keys exist or can't be found in the cache a slice of zero length will be returned
//The ttl will be -1 of no entry was found
func (controller *CacheController) FindSecondaryKeysInCache(cacheKey string) ([]string, time.Duration, error) {

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

//GetPrimaryCacheKey generates the primary cache key for the request according to the requirement in section 4 of RFC7234
//The primary keys is the method, host and effective URI concatenated together
func GetPrimaryCacheKey(cacheConfig *CacheConfig, forwardConfig *ForwardConfig, req *http.Request) string {

	//TODO custom cache keys

	buf := &bytes.Buffer{}

	buf.WriteString(req.Method)
	buf.WriteString(GetEffectiveURI(req, forwardConfig))

	return buf.String()
}

//GetSecondaryCacheKey generates the secondary cache key based on the secondary key fields specified in the cached responses and the current request
func GetSecondaryCacheKey(secondaryKeyFields []string, req *http.Request) string {

	//Sort the fields so the order in the resulting key is always the same
	sort.Strings(secondaryKeyFields)

	buf := &bytes.Buffer{}

	for _, key := range secondaryKeyFields {
		//Separate pieces of the key by the pipe. It is not a allowed value in the Method, hostname, URI or header names so it is a good separator
		buf.WriteRune('|')

		//Write the key as part of the cache key
		buf.WriteString(key)

		//Separate field name from value
		buf.WriteRune(':')

		values := req.Header[textproto.CanonicalMIMEHeaderKey(key)]
		sort.Strings(values)

		for _, value := range values {

			//TODO normalize value based on per header syntax as per Section 4.1 of RFC7234

			buf.WriteString(value)
		}
	}

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
