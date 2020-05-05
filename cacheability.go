package sharedhttpcache

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	AgeHeader          = "Age"
	CacheControlHeader = "Cache-Control"
	ExpiresHeader      = "Expires"
	DateHeader         = "Date"
	VaryHeader         = "Vary"

	NoCacheDirective         = "no-cache"
	NoStoreDirective         = "no-store"
	MustRevalidateDirective  = "must-revalidate"
	ProxyRevalidateDirective = "proxy-revalidate"
	SMaxAgeDirective         = "s-maxage"
	MaxAgeDirective          = "max-age"
	PublicDirective          = "public"
	PrivateDirective         = "private"
)

//shouldStoreResponse determines based on the cache config if this request should be stored
// It determines this based on section 3 of RFC7234
//
// TODO restructure this function so common reasons for no storing a response are checked first
//	this can improve performance a lot
func shouldStoreResponse(config *CacheConfig, resp *http.Response) bool {
	req := resp.Request

	//If the request method is unsafe the response should not be cached
	if !isMethodSafe(config, req.Method) {
		return false
	}

	//If the request method is marked as not cacheable the response should not be cached
	if !isMethodCacheable(config, req.Method) {
		return false
	}

	//If the response is partial and the configuration doesn't permit partial responses don't cache
	if resp.StatusCode == http.StatusPartialContent && !config.CacheIncompleteResponses {
		return false
	}

	requestCacheControlDirectives := splitCacheControlHeader(req.Header[CacheControlHeader])

	//if the request contains the cache-control header and it contains no-store the response should not be cached
	for _, directive := range requestCacheControlDirectives {
		if directive == NoStoreDirective {
			return false
		}
	}

	responseCacheControlDirectives := splitCacheControlHeader(resp.Header[CacheControlHeader])

	for _, directive := range responseCacheControlDirectives {
		//if the response contains the cache-control header and it contains no-store the response should not be cached
		if directive == NoStoreDirective {
			return false
		}

		//if the response contains the cache-control header and it contains private the response should not be cached
		// because this is a shared cache server
		if directive == PrivateDirective {
			return false
		}
	}

	//if the authorization header is set and the cache is shared(which it is)
	// https://tools.ietf.org/html/rfc7234#section-3.2
	if req.Header.Get("Authorization") != "" {

		//Check if the cache-control header in the response allows this
		allowed := false

		for _, directive := range responseCacheControlDirectives {
			if directive == MustRevalidateDirective || directive == PublicDirective {
				allowed = true
			}

			if strings.HasPrefix(directive, SMaxAgeDirective) {
				allowed = true
			}
		}

		//Don't cache unless specificity allowed
		if !allowed {
			return false
		}
	}

	//If the Vary header is a asterisk any variation in the request has a different response
	//Thus it makes the response not cacheable
	if resp.Header.Get(VaryHeader) == "*" {
		return false
	}

	for _, directive := range responseCacheControlDirectives {

		//if the response header Cache-Control contains a s-maxage response directive (see Section 5.2.2.9 of RFC7234)
		//  and the cache is shared (which it is)
		//  the response is cacheable
		if strings.HasPrefix(directive, SMaxAgeDirective) {
			return true
		}

		//if the Cache-Control header contains max-age the response is cacheable (see Section 5.2.2.8 of RFC7234)
		if strings.HasPrefix(directive, MaxAgeDirective) {
			return true
		}

		//if the response contains a public response directive (see Section 5.2.2.5).
		if directive == PublicDirective {
			return true
		}
	}

	//if the expires header is set (see Section 5.3 of RFC7234)
	if resp.Header.Get(ExpiresHeader) != "" {

		expires, err := http.ParseTime(resp.Header.Get(ExpiresHeader))
		if err != nil {

			//If parsing the time gives a error it violates http/1.1
			return false
		}

		//If the expires is in the future, the response is cacheable
		if time.Until(expires) > 0 {
			return true
		}
	}

	//Loop over every file extension to check if it is cacheable by default
	//TODO This comparison may be faster with a string search algorithm like Aho–Corasick
	defaultCacheableExtension := false
	for _, extentsion := range config.CacheableFileExtensions {
		if strings.HasSuffix(req.URL.Path, "."+extentsion) {
			defaultCacheableExtension = true
		}
	}

	if !defaultCacheableExtension {
		return false
	}

	//if the response has a status code that is defined as cacheable by default (see
	//  Section 4.2.2)
	if _, found := config.StatusCodeDefaultExpirationTimes[resp.StatusCode]; found {
		return true
	}

	return false
}

//getResponseTTL checks what the ttl/freshness_lifetime of a response should be based on the config
// and section 4.2.1 of RFC 7234
// if the ttl is negative the response is already stale
func getResponseTTL(config *CacheConfig, resp *http.Response) time.Duration {

	responseAge := getResponseAge(resp)

	//The header value is comma separated, so split it on the comma.
	// Lowercase the directive so string comparison is easier and trim the spaces from the directives
	directives := splitCacheControlHeader(resp.Header[CacheControlHeader])

	//s-maxage has priority because this is a shared cache
	for _, directive := range directives {

		//If the directive starts with s-maxage
		if strings.HasPrefix(directive, SMaxAgeDirective) {

			//Remove the key and equals sign and attempt to parse the remainder as a number
			// This assumes the origin server adheres to the RFC and sends the argument form.
			// TODO check for the quoted-string form
			sMaxAgeString := strings.TrimPrefix(directive, SMaxAgeDirective+"=")
			sMaxAge, err := strconv.ParseInt(sMaxAgeString, 10, 0)

			if err == nil {
				//The remaining TTL is the max age minus the age of the response
				return time.Duration(sMaxAge-responseAge) * time.Second
			}
		}
	}

	for _, directive := range directives {
		//If the directive starts with max-age
		if strings.HasPrefix(directive, MaxAgeDirective) {

			//Remove the key and equals sign and attempt to parse the remainder as a number
			// This assumes the origin server adheres to the RFC and sends the argument form.
			// TODO check for the quoted-string form
			maxAgeString := strings.TrimPrefix(directive, MaxAgeDirective+"=")
			maxAge, err := strconv.ParseInt(maxAgeString, 10, 0)

			if err == nil {
				//The remaining TTL is the max age minus the age of the response
				return time.Duration(maxAge-responseAge) * time.Second
			}
		}
	}

	//Get the date from the response, if not set or invalid make the date the current time
	date := time.Now()
	if dateString := resp.Header.Get(DateHeader); dateString != "" {
		if parsedDate, err := http.ParseTime(dateString); err == nil {
			date = parsedDate
		}
	}

	if expiresString := resp.Header.Get(ExpiresHeader); expiresString != "" {
		expires, err := http.ParseTime(expiresString)

		//If date is invalid it should be assumed to be in the past, Section 5.3 of RFC 7234
		if err != nil {
			return -1
		}

		return expires.Sub(date) - (time.Second * time.Duration(responseAge))
	}

	//Use default values instead of calculating heuristic freshness
	if ttl, found := config.StatusCodeDefaultExpirationTimes[resp.StatusCode]; found {
		return ttl
	}

	return -1
}

//requestOrResponseHasNoCache checks if a response or its request contains a no-cache directive in the Cache-Control header
func requestOrResponseHasNoCache(resp *http.Response) bool {

	for _, directive := range splitCacheControlHeader(resp.Header[CacheControlHeader]) {
		//Check for the plain and field-name form
		//Section 5.2.2.2 of RFC 7234
		if strings.TrimSpace(directive) == NoCacheDirective || strings.HasPrefix(directive, NoCacheDirective+"=") {
			return true
		}
	}

	for _, directive := range splitCacheControlHeader(resp.Request.Header[CacheControlHeader]) {
		if strings.TrimSpace(directive) == NoCacheDirective {
			return true
		}
	}

	//Section 5.4 of RFC 7234
	if resp.Request.Header.Get(CacheControlHeader) == "" && resp.Request.Header.Get("Pragma") == NoCacheDirective {
		return true
	}

	return false
}

//responseHasMustRevalidate checks if a response contains a must-revalidate or proxy-revalidate directive in the Cache-Control header
func responseHasMustRevalidate(resp *http.Response) bool {

	for _, directive := range splitCacheControlHeader(resp.Header[CacheControlHeader]) {
		if strings.TrimSpace(directive) == MustRevalidateDirective || strings.TrimSpace(directive) == ProxyRevalidateDirective {
			return true
		}
	}

	return false
}

//isMethodSafe checks if a request method is safe
func isMethodSafe(config *CacheConfig, method string) bool {
	//Check if the request method is safe
	//TODO This comparison may be faster with a string search algorithm like Aho–Corasick
	for _, safeMethod := range config.SafeMethods {
		if safeMethod == method {
			return true
		}
	}

	return false
}

//isMethodCacheable checks if a request method is cacheable
func isMethodCacheable(config *CacheConfig, method string) bool {

	//Check if the request method is in the list of cacheable methods
	//TODO This comparison may be faster with a string search algorithm like Aho–Corasick
	for _, configMethod := range config.CacheableMethods {
		if configMethod == method {
			return true
		}
	}

	return false
}

// //isResponseCacheableByExtension checks if a response is cacheable based on supported Cache-Control extensions
// // https://tools.ietf.org/html/rfc7234#section-5.2.3
// func isResponseCacheableByExtension(config *CacheConfig, resp *http.Response) bool {
// 	//TODO find and implement cache extension
// 	return false
// }

//mayServeStaleResponse checks if according to the config and rules specified in RFC7234 the caching server is allowed to serve the response if it is stale
func mayServeStaleResponse(cacheConfig *CacheConfig, response *http.Response) bool {

	//If serving of stale responses is turned off
	if !cacheConfig.ServeStaleOnError {
		return false
	}

	if mayServeStaleResponseByExtension(cacheConfig, response) {
		return true
	}

	directives := splitCacheControlHeader(response.Header[CacheControlHeader])
	for _, directive := range directives {

		//If response contains a cache directive that disallowes stale responses section 4.2.4 of RFC7234
		if directive == MustRevalidateDirective || directive == ProxyRevalidateDirective ||
			directive == NoCacheDirective || strings.HasPrefix(directive, SMaxAgeDirective) {

			return false
		}
	}

	return true
}

//mayServeStaleResponseByExtension checks if there are any Cache-Control extensions which allow stale responses to be served
func mayServeStaleResponseByExtension(cacheConfig *CacheConfig, response *http.Response) bool {

	//TODO implement https://tools.ietf.org/html/rfc5861

	return false
}
