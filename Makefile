.DEFAULT_GOAL            	:= help #Sets default target

intergrationtest: ## Executes the intergration tests. The intergration test starts a http client, server and the shared cache and tests the full function of the cache
	# Cleanup old test files
	rm test_output/http_cache_test_coverage.out
	rm test_output/httpcachetest
	# Clone the cache-tests if the path doesn't alrady exist
	ls test_output/cache-tests &> /dev/null || git clone https://github.com/http-tests/cache-tests.git test_output/cache-tests
	# Check out a known good commit
	git -C test_output/cache-tests checkout -f eb4cac0bdd681a1783b194561ae333f40156a299
	# Install dependencies
	npm i --prefix test_output/cache-tests
	# Build the standalone executable as test with coverage export enabled
	go test -tags httpcachetest -covermode count -coverpkg github.com/dylandreimerink/sharedhttpcache,github.com/dylandreimerink/sharedhttpcache/layer -c -o test_output/httpcachetest github.com/dylandreimerink/sharedhttpcache/cmd/sharedhttpcache
	# Start the cache-tests origin server
	npm run server --prefix test_output/cache-tests & CACHE_ORIGIN_PID=$$!; \
	./test_output/httpcachetest -test.coverprofile test_output/http_cache_test_coverage.out __DEVEL--i-heard-you-like-tests --config cmd/sharedhttpcache/cache_test_config.yaml & CACHE_SERVER_PID=$$!; \
	npm run --prefix test_output/cache-tests --silent cli --base=http://localhost:8081 > test_output/cache-tests/results/sharedhttpcache.json || kill $$CACHE_SERVER_PID || true && kill $$CACHE_ORIGIN_PID || true; \
	kill "$$CACHE_SERVER_PID" || true; \
	kill "$$CACHE_ORIGIN_PID" || true
	# Make sure the intergration tests were successfull
	go run cmd/cachetestverifier/main.go test_output/cache-tests/results/sharedhttpcache.json
	

intergrationtestresults: intergrationtest ## Executes the external intergration tests and displays the results in the browser
	go tool cover -html=test_output/http_cache_test_coverage.out
	echo 'export default [{"file": "sharedhttpcache.json","name": "Shared HTTP cache","type": "rev-proxy"}]' > test_output/cache-tests/results/index.mjs
	npm run server --prefix test_output/cache-tests & sleep 3 && xdg-open 'http://localhost:8000/' && sleep 3 && kill $$!

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
