package caching

import (
	"net/http"
	"time"
)

//CacheConfig defines a config for how the cache should behave
// A different cache config can be used on a per request basis like for different websites or pages
type CacheConfig struct {

	//CacheableMethods is a list of request methods for which responses may be cached.
	// It is not advisable to cache unsafe methods like POST. Tho it is possable to do so
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
	// File extentions are used instead of MIME types because the same file extension can have separate MIME types
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
	// Note that this carries a performace impact because at every time a new incomplete range is received reconstruction of the full resource will be attempted
	CombinePartialResponses bool

	//If ShowCacheOnError is true the cache will show a stale response in case revalidation fails because the origin server returned a 5xx code or is unreachable
	ShowCacheOnError bool

	//If HTTPWarnings is true warnings as described in section 5.5 of RFC7234 will be added to HTTP responses
	// This is a option because the feature will be removed from future HTTP specs https://github.com/httpwg/http-core/issues/139
	HTTPWarnings bool
}

//NewCacheConfig creates a new CacheConfig struct which is configures with good defaults which satisfy RFC7234
func NewCacheConfig() *CacheConfig {
	return &CacheConfig{
		CacheableMethods: []string{http.MethodGet}, //A good default according to section 2 of RFC7234

		SafeMethods: []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace}, //section 4.2.1 of RFC7231

		CacheIncompleteResponses: false, //Disable this feature by default because it impacts performace
		CombinePartialResponses:  false, //Disable this feature by default because it impacts performance

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
