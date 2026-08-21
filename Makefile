BINARY := miragate
OUT := ../$(BINARY)
LDFLAGS := -s -w

.PHONY: build test vet run clean cross

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(OUT) ./cmd/miragate

test:
	go test ./...

vet:
	go vet ./...

run: build
	$(OUT) serve

clean:
	rm -f $(OUT)

# 交叉编译常见平台到 ../dist/
cross:
	mkdir -p ../dist
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o ../dist/$(BINARY)-linux-amd64   ./cmd/miragate
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o ../dist/$(BINARY)-linux-arm64   ./cmd/miragate
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o ../dist/$(BINARY)-darwin-arm64  ./cmd/miragate
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o ../dist/$(BINARY)-darwin-amd64  ./cmd/miragate
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o ../dist/$(BINARY)-windows-amd64.exe ./cmd/miragate
