BINARY := tctl
DIST := .bin
LDFLAGS := -s -w

.PHONY: all build linux test clean

all: build linux

build:
	@mkdir -p $(DIST)
	go build -trimpath -ldflags='$(LDFLAGS)' -o $(DIST)/$(BINARY) .

linux:
	@mkdir -p $(DIST)
	GOOS=linux GOARCH=amd64 \
		go build -trimpath -ldflags='$(LDFLAGS)' \
		-o $(DIST)/$(BINARY)-linux-amd64 .

test:
	go test ./...

clean:
	rm -rf $(DIST)
