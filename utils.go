package sharedhttpcache

import "strings"

//SplitCacheControlHeader splits the directives from the Cache-Control header value
func SplitCacheControlHeader(headerValue string) []string {
	directives := []string{}
	for _, directive := range strings.Split(strings.ToLower(headerValue), ",") {
		directives = append(directives, strings.TrimSpace(directive))
	}

	return directives
}
