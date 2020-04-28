package sharedhttpcache

import "strings"

//SplitCacheControlHeader splits the directives from the Cache-Control header value
func SplitCacheControlHeader(headerValues []string) []string {
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
