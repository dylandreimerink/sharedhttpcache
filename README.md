# HTTP caching

The goal of this project is to make a RFC 7234 compliant shared caching server in Go. Tho the main goal is to have a out-of-the-box working caching server it is also important that the functionality is exported so it can be used as library in bigger projects.

## Features

- Flexible configuration
- Multi layer system
- Customizable logging

## Examples

TODO make some usage examples

## TODO

- Make fully RFC7234 compliant
- Adding tests, both unit and integration
- Add project to CI pipeline with code standards
- Store partial responses
- Combining Partial Content
- Calculating Heuristic Freshness based on past behavior
- Refactor code to improve readability
- Add informational headers about cache hit's ect.
- Add HTTP/2 support
- Add Cache-Control extensions (Or at least make a callback so someone can from outside the package)
- Add metrics (prometheus)
- Add user triggered cache invalidation
- Add advanced cache replacement policies to inmemory layer (https://en.wikipedia.org/wiki/Cache_replacement_policies)
- Add disk storage layer
- Add redis storage layer
- Add s3 storage layer
