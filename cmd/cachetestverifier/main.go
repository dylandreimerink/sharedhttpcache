package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
)

var requiredTests = []string{
	"304-etag-update-response-Cache-Control",
	"304-etag-update-response-Clear-Site-Data",
	"304-etag-update-response-Content-Foo",
	"304-etag-update-response-Content-Location",
	"304-etag-update-response-Content-MD5",
	"304-etag-update-response-Content-Security-Policy",
	"304-etag-update-response-Content-Type",
	"304-etag-update-response-Expires",
	"304-etag-update-response-Public-Key-Pins",
	"304-etag-update-response-Set-Cookie",
	"304-etag-update-response-Set-Cookie2",
	"304-etag-update-response-Test-Header",
	"304-etag-update-response-X-Content-Foo",
	"304-etag-update-response-X-Frame-Options",
	"304-etag-update-response-X-Test-Header",
	"304-etag-update-response-X-XSS-Protection",
	"304-etag-update-stored-Cache-Control",
	"304-etag-update-stored-Clear-Site-Data",
	"304-etag-update-stored-Content-Foo",
	"304-etag-update-stored-Content-Location",
	"304-etag-update-stored-Content-MD5",
	"304-etag-update-stored-Content-Security-Policy",
	"304-etag-update-stored-Content-Type",
	"304-etag-update-stored-Expires",
	"304-etag-update-stored-Public-Key-Pins",
	"304-etag-update-stored-Set-Cookie",
	"304-etag-update-stored-Set-Cookie2",
	"304-etag-update-stored-Test-Header",
	"304-etag-update-stored-X-Content-Foo",
	"304-etag-update-stored-X-Frame-Options",
	"304-etag-update-stored-X-Test-Header",
	"304-etag-update-stored-X-XSS-Protection",
	"304-lm-update-response-Cache-Control",
	"304-lm-update-response-Clear-Site-Data",
	"304-lm-update-response-Content-Foo",
	"304-lm-update-response-Content-Location",
	"304-lm-update-response-Content-MD5",
	"304-lm-update-response-Content-Security-Policy",
	"304-lm-update-response-Content-Type",
	"304-lm-update-response-Expires",
	"304-lm-update-response-Public-Key-Pins",
	"304-lm-update-response-Set-Cookie",
	"304-lm-update-response-Set-Cookie2",
	"304-lm-update-response-Test-Header",
	"304-lm-update-response-X-Content-Foo",
	"304-lm-update-response-X-Frame-Options",
	"304-lm-update-response-X-Test-Header",
	"304-lm-update-response-X-XSS-Protection",
	"304-lm-update-stored-Cache-Control",
	"304-lm-update-stored-Clear-Site-Data",
	"304-lm-update-stored-Content-Foo",
	"304-lm-update-stored-Content-Location",
	"304-lm-update-stored-Content-MD5",
	"304-lm-update-stored-Content-Security-Policy",
	"304-lm-update-stored-Content-Type",
	"304-lm-update-stored-Expires",
	"304-lm-update-stored-Public-Key-Pins",
	"304-lm-update-stored-Set-Cookie",
	"304-lm-update-stored-Set-Cookie2",
	"304-lm-update-stored-Test-Header",
	"304-lm-update-stored-X-Content-Foo",
	"304-lm-update-stored-X-Frame-Options",
	"304-lm-update-stored-X-Test-Header",
	"304-lm-update-stored-X-XSS-Protection",
	"304-lm-use-stored-Test-Header",
	"age-parse-dup-0",
	"age-parse-dup-0-twoline",
	"age-parse-dup-old",
	"age-parse-float",
	"age-parse-negative",
	"age-parse-nonnumeric",
	"age-parse-numeric-parameter",
	"age-parse-numeric-parameter-under",
	"age-parse-parameter",
	"age-parse-prefix",
	"age-parse-prefix-twoline",
	"age-parse-suffix",
	"cc-resp-no-cache",
	"cc-resp-no-cache-case-insensitive",
	"cc-resp-no-cache-revalidate-fresh",
	"cc-resp-no-store",
	"cc-resp-no-store-case-insensitive",
	"cc-resp-no-store-fresh",
	"cc-resp-private-shared",
	"ccreq-ma0",
	"ccreq-ma1",
	"ccreq-magreaterage",
	"ccreq-max-stale",
	"ccreq-min-fresh",
	"ccreq-min-fresh-age",
	"ccreq-no-cache",
	"ccreq-no-cache-etag",
	"ccreq-no-cache-lm",
	"conditional-etag-forward",
	"conditional-etag-strong-generate",
	"conditional-etag-weak-generate-weak",
	"freshness-expires-age-fast-date",
	"freshness-expires-age-slow-date",
	"freshness-expires-future",
	"freshness-expires-invalid",
	"freshness-expires-invalid-1-digit-hour",
	"freshness-expires-invalid-date",
	"freshness-expires-invalid-multiple-spaces",
	"freshness-expires-old-date",
	"freshness-expires-past",
	"freshness-expires-present",
	"freshness-expires-wrong-case-month",
	"freshness-expires-wrong-case-weekday",
	"freshness-max-age",
	"freshness-max-age-0",
	"freshness-max-age-0-expires",
	"freshness-max-age-100a",
	"freshness-max-age-a100",
	"freshness-max-age-age",
	"freshness-max-age-case-insenstive",
	"freshness-max-age-expires",
	"freshness-max-age-expires-invalid",
	"freshness-max-age-extension",
	"freshness-max-age-ignore-quoted",
	"freshness-max-age-ignore-quoted-rev",
	"freshness-max-age-max",
	"freshness-max-age-max-minus-1",
	"freshness-max-age-max-plus",
	"freshness-max-age-max-plus-1",
	"freshness-max-age-negative",
	"freshness-max-age-quoted",
	"freshness-max-age-s-maxage-shared-longer",
	"freshness-max-age-s-maxage-shared-longer-multiple",
	"freshness-max-age-s-maxage-shared-longer-reversed",
	"freshness-max-age-s-maxage-shared-shorter",
	"freshness-max-age-s-maxage-shared-shorter-expires",
	"freshness-max-age-single-quoted",
	"freshness-none",
	"freshness-s-maxage-shared",
	"headers-omit-headers-listed-in-Cache-Control-no-cache",
	"headers-omit-headers-listed-in-Cache-Control-no-cache-single",
	"headers-omit-headers-listed-in-Connection",
	"headers-store-Clear-Site-Data",
	"headers-store-Content-Type",
	"headers-store-Public-Key-Pins",
	"headers-store-Set-Cookie",
	"headers-store-Set-Cookie2",
	"headers-store-Strict-Transport-Security",
	"headers-store-Strict-Transport-Security2",
	"headers-store-Test-Header",
	"headers-store-WWW-Authenticate",
	"headers-store-X-Frame-Options",
	"headers-store-X-Test-Header",
	"headers-store-X-XSS-Protection",
	"heuristic-201-not_cached",
	"heuristic-202-not_cached",
	"heuristic-403-not_cached",
	"heuristic-502-not_cached",
	"heuristic-503-not_cached",
	"heuristic-504-not_cached",
	"heuristic-599-not_cached",
	"invalidate-DELETE",
	"invalidate-DELETE-cl",
	"invalidate-DELETE-failed",
	"invalidate-DELETE-location",
	"invalidate-M-SEARCH",
	"invalidate-M-SEARCH-cl",
	"invalidate-M-SEARCH-failed",
	"invalidate-M-SEARCH-location",
	"invalidate-POST",
	"invalidate-POST-cl",
	"invalidate-POST-failed",
	"invalidate-POST-location",
	"invalidate-PUT",
	"invalidate-PUT-cl",
	"invalidate-PUT-failed",
	"invalidate-PUT-location",
	"other-age-gen",
	"other-age-update-expires",
	"other-age-update-max-age",
	"other-cookie",
	"other-date-update",
	"other-fresh-content-disposition-attachment",
	"other-set-cookie",
	"partial-store-partial-reuse-partial",
	"pragma-request-extension",
	"pragma-response-extension",
	"pragma-response-no-cache",
	"query-args-different",
	"query-args-same",
	"status-200-fresh",
	"status-200-stale",
	"status-203-fresh",
	"status-203-stale",
	"status-204-fresh",
	"status-204-stale",
	"status-299-fresh",
	"status-299-stale",
	"status-301-fresh",
	"status-301-stale",
	"status-302-fresh",
	"status-302-stale",
	"status-303-fresh",
	"status-303-stale",
	"status-307-fresh",
	"status-307-stale",
	"status-308-fresh",
	"status-308-stale",
	"status-400-fresh",
	"status-400-stale",
	"status-404-fresh",
	"status-404-stale",
	"status-410-fresh",
	"status-410-stale",
	"status-499-fresh",
	"status-499-stale",
	"status-500-fresh",
	"status-500-stale",
	"status-502-fresh",
	"status-502-stale",
	"status-503-fresh",
	"status-503-stale",
	"status-504-fresh",
	"status-504-stale",
	"status-599-fresh",
	"status-599-stale",
	"surrogate-max-age-0",
	"surrogate-max-age-age",
	"surrogate-max-age-other-target",
	"surrogate-max-age-space-after-equals",
	"surrogate-max-age-space-before-equals",
	"surrogate-no-store",
	"surrogate-remove-header",
	"vary-2-match",
	"vary-2-match-omit",
	"vary-2-no-match",
	"vary-3-match",
	"vary-3-no-match",
	"vary-3-omit",
	"vary-3-order",
	"vary-cache-key",
	"vary-invalidate",
	"vary-match",
	"vary-no-match",
	"vary-omit",
	"vary-star",
	"vary-syntax-empty-star",
	"vary-syntax-empty-star-lines",
	"vary-syntax-foo-star",
	"vary-syntax-star-foo",
	"vary-syntax-star-star",
}

//This is a small tool that checks the contents of a integration test output file against a list of tests which should be successful
func main() {

	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s {test-result.json}\n", os.Args[0])
		os.Exit(1)
	}

	fileContent, err := ioutil.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}

	var contents map[string]interface{}
	err = json.Unmarshal([]byte(fileContent), &contents)
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}

	failed := false

	for _, name := range requiredTests {
		value, found := contents[name]
		if !found {
			fmt.Fprintf(os.Stderr, "Missing required test '%s' in test results\n", name)
			failed = true
		}

		if valBool, ok := value.(bool); !ok || !valBool {
			fmt.Fprintf(os.Stderr, "Required test '%s' failed\n", name)
			failed = true
		}
	}

	if failed {
		os.Exit(1)
	}
}
