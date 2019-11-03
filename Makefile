.DEFAULT_GOAL            	:= help #Sets default target

systemtest: ## Execute a system test. The system test starts a http client, server and the shared cache and tests the full function of the cache
	go test -tags systemtest -covermode count -coverpkg github.com/dylandreimerink/sharedhttpcache,github.com/dylandreimerink/sharedhttpcache/layer -c -o test_output/systemtests github.com/dylandreimerink/sharedhttpcache/cmd/systemtests
	./test_output/systemtests -test.coverprofile test_output/system_test_coverage.out __DEVEL--i-heard-you-like-tests
	go tool cover -html=test_output/system_test_coverage.out

test: ## Executes the unit tests and opens the coverage report in the default browser
	go test ./... -covermode count -coverprofile test_output/unit_test_coverage.out
	go tool cover -html=test_output/unit_test_coverage.out

# Extra
help: ## Show help text
	@make --help
	@make print-hr
	@echo "Targets:"
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-$(makeCommentSpacing)s\033[0m %s\n", $$1, $$2}'

print-hr: # Print horizontal line
	@printf %"$$(tput cols)"s |tr " " "-"
