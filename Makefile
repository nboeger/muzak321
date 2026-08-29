VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//' || echo dev)

LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: all build test vet clean install

all: build

graphify:
	 graphify extract .  --backend ollama --model llama3.1:8b

build:
	go build $(LDFLAGS) -o muzak321 .

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f muzak321

install:
	go install $(LDFLAGS) .
