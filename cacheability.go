package sharedhttpcache

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

//ShouldStoreResponse determins based on the cache config if this request should be stored
// It determins this based on section 3 of RFC7234
//
// TODO restructure this function so common reasons for no storing a response are checked first
//	this can improve performance a lot
func ShouldStoreResponse(config *CacheConfig, resp *http.Response) bool {
	req := resp.Request

	//If the request method is unsafe the response should not be cached
	if !IsMethodSafe(config, req.Method) {
		return false
	}

	//If the request method is marked as not cacheable the response should not be cached
	if !IsMethodCacheable(config, req.Method) {
		return false
	}

	//If the response is partial and the configuration doesn't permit partial responses don't cache
	if resp.StatusCode == http.StatusPartialContent && !config.CacheIncompleteResponses {
		return false
	}

	requestCacheControlDirectives := SplitCacheControlHeader(req.Header.Get("Cache-Control"))

	//if the request contains the cache-control header and it contains no-store the response should not be cached
	for _, directive := range requestCacheControlDirectives {
		if directive == "no-store" {
			return false
		}
	}

	responseCacheControlDirectives := SplitCacheControlHeader(resp.Header.Get("Cache-Control"))

	for _, directive := range responseCacheControlDirectives {
		//if the response contains the cache-control header and it contains no-store the response should not be cached
		if directive == "no-store" {
			return false
		}

		//if the response contains the cache-control header and it contains private the response should not be cached
		// because this is a shared cache server
		if directive == "private" {
			return false
		}
	}

	//if the authorization header is set and the cache is shared(which it is)
	// https://tools.ietf.org/html/rfc7234#section-3.2
	if req.Header.Get("Authorization") != "" {

		//Check if the cache-control header in the response allows this
		allowed := false

		for _, directive := range responseCacheControlDirectives {
			if directive == "must-revalidate" || directive == "public" {
				allowed = true
			}

			if strings.HasPrefix(directive, "s-maxage") {
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
	if resp.Header.Get("Vary") == "*" {
		return false
	}

	//if the expires header is set (see Section 5.3 of RFC7234)
	if resp.Header.Get("Expires") != "" {

		expires, err := http.ParseTime(resp.Header.Get("Expires"))
		if err != nil {

			//If parsing the time gives a error it violates http/1.1
			return false
		}

		//If the expires is in the future, the response is cacheable
		if time.Until(expires) > 0 {
			return true
		}
	}

	for _, directive := range responseCacheControlDirectives {

		//if the Cache-Control header contains max-age the response is cacheable (see Section 5.2.2.8 of RFC7234)
		if strings.HasPrefix(directive, "max-age") {
			return true
		}

		//if the response header Cache-Control contains a s-maxage response directive (see Section 5.2.2.9 of RFC7234)
		//  and the cache is shared (which it is)
		//  the response is cacheable
		if strings.HasPrefix(directive, "s-maxage") {
			return true
		}

		//if the response contains a public response directive (see Section 5.2.2.5).
		if directive == "public" {
			return true
		}
	}

	//Loop over every file extension to check if it is cacheable by default
	//TODO This comparason may be faster with a string search algorithm like Aho–Corasick
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

//GetResponseTTL checks what the ttl/freshness_lifetime of a response should be based on the config
// and section 4.2.1 of RFC 7234
// if the ttl is negative the response is already stale
func GetResponseTTL(config *CacheConfig, resp *http.Response) time.Duration {

	//The header value is comma seperated, so split it on the comma.
	// Lowercase the directive so string comparason is easier and trim the spaces from the directives
	directives := SplitCacheControlHeader(resp.Header.Get("Cache-Control"))

	//s-maxage has priority because this is a shared cache
	for _, directive := range directives {

		//If the directive starts with s-maxage
		if strings.HasPrefix(directive, "s-maxage") {

			//Remove the key and equals sign and attempt to parse the remainder as a number
			// This assumes the origin server adheres to the RFC and sends the argument form.
			// TODO check for the quoted-string form
			sMaxAgeString := strings.TrimPrefix(directive, "s-maxage=")
			sMaxAge, err := strconv.ParseInt(sMaxAgeString, 10, 0)

			if err != nil {
				return time.Duration(sMaxAge) * time.Second
			}
		}
	}

	for _, directive := range directives {
		//If the directive starts with max-age
		if strings.HasPrefix(directive, "max-age") {

			//Remove the key and equals sign and attempt to parse the remainder as a number
			// This assumes the origin server adheres to the RFC and sends the argument form.
			// TODO check for the quoted-string form
			maxAgeString := strings.TrimPrefix(directive, "max-age=")
			maxAge, err := strconv.ParseInt(maxAgeString, 10, 0)

			if err != nil {
				return time.Duration(maxAge) * time.Second
			}
		}
	}

	//Get the date from the response, if not set or invalid make the date the current time
	date := time.Now()
	if dateString := resp.Header.Get("Date"); dateString != "" {
		if parsedDate, err := http.ParseTime(dateString); err != nil {
			date = parsedDate
		}
	}

	if expiresString := resp.Header.Get("Expires"); expiresString != "" {
		expires, err := http.ParseTime(expiresString)

		//If date is invalid it should be assumed to be in the past, Section 5.3 of RFC 7234
		if err != nil {
			return -1
		}

		return expires.Sub(date)
	}

	//Use default values instead of caluclating heuristic freshness
	if ttl, found := config.StatusCodeDefaultExpirationTimes[resp.StatusCode]; found {
		return ttl
	}

	return -1
}

func RequestOrResponseHasNoCache(resp *http.Response) bool {

	for _, directive := range SplitCacheControlHeader(resp.Header.Get("Cache-Control")) {
		if strings.TrimSpace(directive) == "no-cache" {
			return true
		}
	}

	for _, directive := range SplitCacheControlHeader(resp.Request.Header.Get("Cache-Control")) {
		if strings.TrimSpace(directive) == "no-cache" {
			return true
		}
	}

	//Section 5.4 of RFC 7234
	if resp.Request.Header.Get("Cache-Control") == "" && resp.Request.Header.Get("Pragma") == "no-cache" {
		return true
	}

	return false
}

//IsMethodSafe checks if a request method is safe
func IsMethodSafe(config *CacheConfig, method string) bool {
	//Check if the request method is safe
	//TODO This comparason may be faster with a string search algorithm like Aho–Corasick
	for _, safeMethod := range config.SafeMethods {
		if safeMethod == method {
			return true
		}
	}

	return false
}

//IsMethodCacheable checks if a request method is cacheable
func IsMethodCacheable(config *CacheConfig, method string) bool {

	//Check if the request method is in the list of cacheable methods
	//TODO This comparason may be faster with a string search algorithm like Aho–Corasick
	for _, configMethod := range config.CacheableMethods {
		if configMethod == method {
			return true
		}
	}

	return false
}

//IsCacheableByExtension checks if a response is cacheable based on supported Cache-Control extensions
// https://tools.ietf.org/html/rfc7234#section-5.2.3
func IsResponseCacheableByExtension(config *CacheConfig, resp *http.Response) bool {
	//TODO find and implement cache extension
	return false
}
