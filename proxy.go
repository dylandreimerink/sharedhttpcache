package sharedhttpcache

import (
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/context"
)

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

//proxyToOrigin proxies a request to a origin server using the given config and return the response
func proxyToOrigin(forwardContext context.Context, transport http.RoundTripper, forwardConfig *ForwardConfig, req *http.Request) (*http.Response, error) {
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

	//Forward the original hostname for which the request was intended
	outreq.URL.Host = req.Host
	outreq.Host = req.Host

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

//writeHTTPResponse writes a response the response writer
func writeHTTPResponse(rw http.ResponseWriter, response *http.Response) error {

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

	apparentAge := int64(0)

	dateString := response.Header.Get(DateHeader)
	if dateString != "" {
		date, err := http.ParseTime(dateString)
		if err == nil {

			//Get the second difference between date and now
			// this is the apparent_age method described in section 4.2.3 of RFC 7234
			apparentAge = int64(time.Since(date).Seconds())
			if apparentAge < 0 {
				apparentAge = 0
			}

		}
	}

	if ageHeader := response.Header.Get(AgeHeader); ageHeader != "" {
		ageValue, err := strconv.ParseInt(ageHeader, 10, 0)
		if err == nil {

			//TODO correct age by adding response_delay

			return ageValue + apparentAge
		}
	}

	return apparentAge
}

//writeCachedResponse writes a cached response to a response writer
// this function should be used to write cached responses because it modifies the response to comply with the RFC's
func writeCachedResponse(rw http.ResponseWriter, cachedResponse *http.Response, ttl time.Duration) error {

	age := getResponseAge(cachedResponse)

	//If the age is positive we add the header. Negative ages are not allowed
	if age >= 0 {
		cachedResponse.Header.Set(AgeHeader, strconv.FormatInt(age, 10))
	}

	return writeHTTPResponse(rw, cachedResponse)
}
