
BUILD_IMAGE  = golang:1.19

build-local:
	go build \
		-v \
		--buildmode=c-shared \
		-o libgolang.so \
		.

build:
	docker run --rm -v $(shell pwd):/work/src -w /work/src ${BUILD_IMAGE} make build-local

.PHONY: build-local build
