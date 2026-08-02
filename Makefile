VERSION ?= dev

LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: all build test vet clean install

all: build

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