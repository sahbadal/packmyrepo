VERSION ?= 0.1.0
LDFLAGS = -s -w

build:
	go build -ldflags "$(LDFLAGS)" -o ctxpack .

test:
	go test ./...

dist:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/ctxpack-windows-amd64.exe .
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/ctxpack-macos-arm64 .
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/ctxpack-macos-amd64 .
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/ctxpack-linux-amd64 .
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/ctxpack-linux-arm64 .

clean:
	rm -rf ctxpack dist
