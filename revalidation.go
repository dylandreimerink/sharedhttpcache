package sharedhttpcache

import (
	"context"
	"net/http"
)

//TODO implement bulk revalidation with If-None-Match precondition

//makeRevalidationRequest makes a revalidation request based on the current request and the stored response to revalidate
// If a conditional request can't be created the response will be nil in which case the cached response should be considered invalidated
func makeRevalidationRequest(request *http.Request, response *http.Response) *http.Request {

	//We use the request we got from the http client.
	//However we can't modify it because we don't know how it's used outside this function
	validationRequest := request.Clone(context.Background())

	canValidate := false

	//If there is a Etag in the response we add the If-None-Match Precondition
	if etag := response.Header.Get("Etag"); etag != "" {
		validationRequest.Header.Set("If-None-Match", etag)
		canValidate = true
	}

	//If-Modified-Since is only allowed for GET and HEAD requests as per Section 3.3 of RFC7232
	if request.Method == "GET" || request.Method == "HEAD" {

		//If the stored response has a last modified header set the If-Modified-Since precondition
		if lastModified := response.Header.Get("Last-Modified"); lastModified != "" {
			validationRequest.Header.Set("If-Modified-Since", lastModified)

			canValidate = true
		}
	}

	if canValidate {
		return validationRequest
	}

	return nil
}
