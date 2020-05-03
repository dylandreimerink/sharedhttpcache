# HTTP caching

[![GoDoc](https://godoc.org/github.com/dylandreimerink/sharedhttpcache?status.svg)](https://godoc.org/github.com/dylandreimerink/sharedhttpcache)
[![Travis CI](https://api.travis-ci.com/dylandreimerink/sharedhttpcache.svg?branch=master)](https://travis-ci.com/dylandreimerink/sharedhttpcache)
[![Codecov](https://codecov.io/gh/dylandreimerink/sharedhttpcache/branch/master/graph/badge.svg)](https://codecov.io/gh/dylandreimerink/sharedhttpcache)
[![GoReport](https://goreportcard.com/badge/github.com/dylandreimerink/sharedhttpcache)](https://goreportcard.com/report/github.com/dylandreimerink/sharedhttpcache)
[![GolangCI](https://golangci.com/badges/github.com/golangci/golangci-lint.svg)](https://golangci.com/r/github.com/dylandreimerink/sharedhttpcache)

The goal of this project is to make a RFC 7234 compliant shared caching server in Go. Tho the main goal is to have a out-of-the-box working caching server it is also important that the functionality is exported so it can be used as library in bigger projects.

## Features

- Flexible configuration
- Multi layer system
- Customizable logging

## Usage

TODO make command line usage section for the standalone cache server

## Examples

For library examples please go the the [godoc page](https://godoc.org/github.com/dylandreimerink/sharedhttpcache)

## Validation

To validate the cache a testing harness like https://github.com/http-tests/cache-tests can be used.

## TODO

- Make fully RFC7234 compliant
- Add standalone cache server command for use as executable
- Adding tests, both unit and integration
- Store partial responses
- Combining Partial Content
- Calculating Heuristic Freshness based on past behavior
- Refactor code to improve readability
- Add informational headers about cache hit's ect.
- Add websocket support
- Add HTTP/2 push support
- Add optional [RFC7239](https://tools.ietf.org/html/rfc7239) support
- http cache-aware server-push [link](https://github.com/h2o/h2o/issues/421)
- Add Cache-Control extensions (Or at least make a callback so someone can from outside the package)
  - [RFC5861 - HTTP Cache-Control Extensions for Stale Content](https://tools.ietf.org/html/rfc5861)
  - [RFC8246 - HTTP Immutable Responses](https://tools.ietf.org/html/rfc8246)
- Add metrics (prometheus)
- Add user triggered cache invalidation
- Add advanced [cache replacement policies](https://en.wikipedia.org/wiki/Cache_replacement_policies) to inmemory layer
- Add disk storage layer
- Add redis storage layer
- Add s3 storage layer
