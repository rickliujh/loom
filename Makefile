VERSION		?= dev
PROJ_BIN_PATH	:= ./bin/
BINARY_NAME	:= loom

all: help

.PHONY: build
build: ## Compile binary
	go build -v -ldflags="-X 'github.com/rickliujh/loom/cmd.Version=$(VERSION)'" -o $(PROJ_BIN_PATH)$(BINARY_NAME) .

.PHONY: build-prod
build-prod: ## Compile production binary (static, stripped)
	CGO_ENABLED=0 go build -v -ldflags='-w -s -X "github.com/rickliujh/loom/cmd.Version=$(VERSION)" -extldflags "-static"' -o $(PROJ_BIN_PATH)$(BINARY_NAME) .

.PHONY: tidy
tidy: ## Update go modules
	go mod tidy

.PHONY: upgrade
upgrade: ## Upgrade all dependencies
	go get -d -u ./...
	go mod tidy

.PHONY: test
test: ## Run tests, GO_TEST_ARGS for extra args
	go test -count=1 -race -covermode=atomic -coverprofile=cover.out $$GO_TEST_ARGS ./...

.PHONY: lint
lint: ## Lint the project
	golangci-lint run

.PHONY: vulncheck
vulncheck: ## Check for source vulnerabilities
	govulncheck -test ./...

.PHONY: run
run: ## Run loom without compiling (use ARGS, e.g. make run ARGS="run ./example --dry-run -p serviceName=test")
	go run main.go $(ARGS)

.PHONY: tag
tag: ## Create release tag
	git tag -s -m "version bump to $(VERSION)" $(VERSION)
	git push origin $(VERSION)

.PHONY: tagless
tagless: ## Delete the current release tag
	git tag -d $(VERSION)
	git push --delete origin $(VERSION)

.PHONY: clean
clean: ## Clean bin and temp directories
	go clean
	rm -fr $(PROJ_BIN_PATH)
	rm -f cover.out

.PHONY: help
help: ## Display available commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk \
		'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
