# Keep targets in sync with .github/workflows/ci.yml (T002).
GO ?= go
LDFLAGS := -s -w -X main.version=$(or $(VERSION),dev)
GOFILES = $(shell git ls-files '*.go')
# IPv4 CIDR literals are allowed only in test code and internal/contract/private.go (FR-006).
CIDR_RE := [0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}

.PHONY: lint test test-contract test-integration build-server build-collector hwtest

lint:
	@out="$$(gofmt -l $(GOFILES))"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...
	@if git grep -nE '$(CIDR_RE)' -- '*.go' ':!*_test.go' ':!internal/contract/private.go'; then \
		echo "subnet literals found outside internal/contract/private.go"; exit 1; fi

test:
	$(GO) test ./internal/... ./cmd/...

test-contract:
	$(GO) test ./tests/contract/...

test-integration:
	$(GO) test ./tests/integration/...

build-server:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/hne-server-linux-amd64 ./cmd/hne-server
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/hne-server-linux-arm64 ./cmd/hne-server

build-collector:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/hne-collector-windows-amd64.exe ./cmd/hne-collector
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/hne-collector-linux-amd64 ./cmd/hne-collector
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/hne-collector-linux-arm64 ./cmd/hne-collector

hwtest:
	$(GO) test -tags hwtest ./internal/collect/...
