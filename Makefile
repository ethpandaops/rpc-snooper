# assertoor
VERSION := $(shell git rev-parse --short HEAD)
GOLDFLAGS += -X 'github.com/ethpandaops/rpc-snooper/utils.BuildVersion="$(VERSION)"'
GOLDFLAGS += -X 'github.com/ethpandaops/rpc-snooper/utils.BuildRelease="$(RELEASE)"'

.PHONY: all test clean

all: build

test:
	go test ./...

build:
	@echo version: $(VERSION)
	env CGO_ENABLED=1 go build -v -o bin/ -ldflags="-s -w $(GOLDFLAGS)" ./cmd/*

clean:
	rm -f bin/*
