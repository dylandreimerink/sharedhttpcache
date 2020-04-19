package sharedhttpcache

import (
	"net/http"
	"time"
)

//CacheConfig defines a config for how the cache should behave
// A different cache config can be used on a per request basis like for different websites or pages
type CacheConfig struct {

	//CacheableMethods is a list of request methods for which responses may be cached.
	// It is not advisable to cache unsafe methods like POST. Tho it is possible to do so
	// Note that unsafe methods will not be cached even if they are in this list as per section 4 of RFC7234
	//
	// WARNING values must be uppercase, no case conversion is done at runtime
	CacheableMethods []string

	//SafeMethods is a list of "safe" request methods as defined in section 4.2.1 of RFC7231
	// Any method not in this list is considered unsafe and will not be cached
	// If a request is made with a unsafe method it may cause invalidation as per section 4.4 of RFC7234
	SafeMethods []string

	//StatusCodeDefaultExpirationTimes is a map of times index by the http response code
	//
	// These times will be used as default expiration time unless the response contains a header which specifies a different
	//
	// Not all responses should be cached for the same duration.
	// A 307 Temporary Redirect for example should be cached for less time than a 301 Moved Permanently
	//
	// Codes not appearing in this list will be considered NOT understood and thus make a response uncacheable according to section 3 of RFC7234.
	StatusCodeDefaultExpirationTimes map[int]time.Duration

	//CacheableFileExtensions is a list of cacheable file extensions
	// File extensions are used instead of MIME types because the same file extension can have separate MIME types
	// It is advised to only use static file types like stylesheets or images and not dynamic content like html
	CacheableFileExtensions []string

	//CacheIncompleteResponses enables or disables the optional feature mentioned in section 3.1 of RFC7234
	// Caching of incomplete requests will cache responses with status code 206 (Partial Content)
	//
	// Note that this carries a performance impact because ranges have to be accounted for when storing and querying the cached content
	CacheIncompleteResponses bool

	//CombinePartialResponses enables or disables the optional feature mentioned in section 3.3 of RFC7234
	// When this feature is enabled and incomplete responses are enabled
	// the caching server attempts to combine multiple incomplete responses into a complete response.
	//
	// Note that this carries a performance impact because at every time a new incomplete range is received reconstruction of the full resource will be attempted
	CombinePartialResponses bool

	//If ServeStaleOnError is true the cache will attempt to serve a stale response in case revalidation fails because the origin server returned a 5xx code or is unreachable
	//This setting respects the Cache-Control header of the client and server.
	ServeStaleOnError bool

	//If HTTPWarnings is true warnings as described in section 5.5 of RFC7234 will be added to HTTP responses
	// This is a option because the feature will be removed from future HTTP specs https://github.com/httpwg/http-core/issues/139
	HTTPWarnings bool
}

//NewCacheConfig creates a new CacheConfig struct which is configures with good defaults which satisfy RFC7234
func NewCacheConfig() *CacheConfig {
	return &CacheConfig{
		CacheableMethods: []string{http.MethodGet}, //A good default according to section 2 of RFC7234

		SafeMethods: []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace}, //section 4.2.1 of RFC7231

		CacheIncompleteResponses: false, //Disable this feature by default because it impacts performance
		CombinePartialResponses:  false, //Disable this feature by default because it impacts performance

		ServeStaleOnError: true, //No reason not to do it by default

		HTTPWarnings: true, //Be RFC compliant by default

		CacheableFileExtensions: []string{ //Default used by CloudFlare
			"bmp", "ejs", "jpeg", "pdf", "ps", "ttf",
			"class", "eot", "jpg", "pict", "svg", "webp",
			"css", "eps", "js", "pls", "svgz", "woff",
			"csv", "gif", "mid", "png", "swf", "woff2",
			"doc", "ico", "midi", "ppt", "tif", "xls",
			"docx", "jar", "otf", "pptx", "tiff", "xlsx",
		},

		StatusCodeDefaultExpirationTimes: map[int]time.Duration{
			200: 2 * time.Hour,
			206: 2 * time.Hour,
			301: 2 * time.Hour,
			302: 20 * time.Minute,
			303: 20 * time.Minute,
			403: 1 * time.Minute,
			404: 3 * time.Minute,
			410: 3 * time.Minute,
		},
	}
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

//The TransportResolverFunc type is an adapter to allow the use of ordinary functions as TransportResolver
type TransportResolverFunc func(req *http.Request) http.RoundTripper

//GetForwardConfig calls the underlying function to resolve a round tripper from a request
func (resolver TransportResolverFunc) GetTransport(req *http.Request) http.RoundTripper {
	return resolver(req)
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

//The ForwardConfigResolverFunc type is an adapter to allow the use of ordinary functions as ForwardConfigResolver
type ForwardConfigResolverFunc func(req *http.Request) *ForwardConfig

//GetForwardConfig calls the underlying function to resolve a forward config from a request
func (resolver ForwardConfigResolverFunc) GetForwardConfig(req *http.Request) *ForwardConfig {
	return resolver(req)
}
