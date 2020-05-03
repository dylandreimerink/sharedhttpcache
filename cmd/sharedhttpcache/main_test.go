// +build httpcachetest

//Credits to Aleksa Sarai, Source: https://www.cyphar.com/blog/post/20170412-golang-integration-coverage

package main

import (
	"os"
	"strings"
	"testing"
)

func TestMain(t *testing.T) {
	var (
		args []string
		run  bool
	)

	for _, arg := range os.Args {
		switch {
		case arg == "__DEVEL--i-heard-you-like-tests":
			run = true
		case strings.HasPrefix(arg, "-test"):
		case strings.HasPrefix(arg, "__DEVEL"):
		default:
			args = append(args, arg)
		}
	}
	os.Args = args

	if run {
		main()
	}
}
