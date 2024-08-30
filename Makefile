
.PHONY: build

build:
	go build -v --buildmode=c-shared -buildvcs=false -o libgolang.so .
