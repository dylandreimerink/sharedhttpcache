package sharedhttpcache

import (
	"bytes"
	"net/http"
	"net/textproto"
	"net/url"
	"sort"
	"strings"
)

//splitCacheControlHeader splits the directives from the Cache-Control header value
func splitCacheControlHeader(headerValues []string) []string {
	directives := []string{}
	for _, headerValue := range headerValues {
		inQuote := false
		curDir := ""
		for _, char := range strings.ToLower(headerValue) {
			if char == '"' {
				inQuote = !inQuote
			}

			if char == ',' && !inQuote {
				trimmed := strings.TrimSpace(curDir)
				if len(trimmed) != 0 {
					directives = append(directives, trimmed)
				}
				curDir = ""
				continue
			}

			curDir += string(char)
		}

		trimmed := strings.TrimSpace(curDir)
		if len(trimmed) != 0 {
			directives = append(directives, trimmed)
		}
	}

	return directives
}

//getPrimaryCacheKey generates the primary cache key for the request according to the requirement in section 4 of RFC7234
//The primary keys is the method, host and effective URI concatenated together
func getPrimaryCacheKey(cacheConfig *CacheConfig, forwardConfig *ForwardConfig, req *http.Request) string {

	//TODO custom cache keys

	buf := &bytes.Buffer{}

	buf.WriteString(req.Method)
	buf.WriteString(getEffectiveURI(req, forwardConfig))

	return buf.String()
}

//getSecondaryCacheKey generates the secondary cache key based on the secondary key fields specified in the cached responses and the current request
func getSecondaryCacheKey(secondaryKeyFields []string, req *http.Request) string {

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

//getEffectiveURI returns the effective URI as string generated from a request object
// https://tools.ietf.org/html/rfc7230#section-5.5
func getEffectiveURI(req *http.Request, forwardConfig *ForwardConfig) string {

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
