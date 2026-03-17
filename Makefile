GO ?= go
GOCACHE ?= $(PWD)/.gocache
GOMODCACHE ?= $(PWD)/.gocache/mod
CACHE_ENV = GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE)

.PHONY: all fmt test build plugin integ integ-orchestration clean

all: test

fmt:
	$(GO) fmt ./...

test:
	$(CACHE_ENV) $(GO) test -count=1 ./...

build:
	$(CACHE_ENV) $(GO) build ./...

plugin:
	$(CACHE_ENV) $(GO) build -o bin/orchestrationplugin ./cmd/orchestrationplugin

integ-orchestration:
	$(CACHE_ENV) $(GO) run ./integ/orchestration.go

integ: integ-orchestration

clean:
	rm -rf $(GOCACHE) bin
