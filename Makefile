VERSION ?= dev
LDFLAGS = -s -w -X main.version=$(VERSION)

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o packmyrepo .

test:
	go test ./...

# Build the npm packages locally (no publishing): make npm VERSION=0.1.0
npm:
	node npm/build.mjs $(VERSION)

clean:
	rm -rf packmyrepo packmyrepo.exe dist npm/dist

.PHONY: build test npm clean