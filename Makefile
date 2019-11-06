.DEFAULT_GOAL            	:= help #Sets default target

intergrationtest: ## Executes the intergration tests. The intergration test starts a http client, server and the shared cache and tests the full function of the cache
	go test -tags intergrationtest -covermode count -coverpkg github.com/dylandreimerink/sharedhttpcache,github.com/dylandreimerink/sharedhttpcache/layer -c -o test_output/intergrationtests github.com/dylandreimerink/sharedhttpcache/cmd/intergrationtests
	./test_output/intergrationtests -test.coverprofile test_output/intergration_test_coverage.out __DEVEL--i-heard-you-like-tests
	go tool cover -html=test_output/intergration_test_coverage.out

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
