VERSION ?= 0.1.0
LDFLAGS = -s -w

build:
	go build -ldflags "$(LDFLAGS)" -o packmyrepo .

test:
	go test ./...

dist:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/packmyrepo-windows-amd64.exe .
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/packmyrepo-macos-arm64 .
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/packmyrepo-macos-amd64 .
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/packmyrepo-linux-amd64 .
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/packmyrepo-linux-arm64 .

clean:
	rm -rf packmyrepo dist
