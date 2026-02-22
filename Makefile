BINARY := ffmpeg-benchmark
VERSION := $(shell cat VERSION)
DIST_DIR := dist
CMD := ./cmd/ffmpeg-benchmark

.PHONY: build test run clean dist

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o $(BINARY) $(CMD)

test:
	go test ./...

run: build
	./$(BINARY) run

clean:
	rm -rf $(BINARY) $(DIST_DIR)

dist:
	mkdir -p $(DIST_DIR)
	for target in \
		linux/amd64 \
		linux/arm64 \
		darwin/amd64 \
		darwin/arm64; do \
			GOOS=$${target%/*}; \
			GOARCH=$${target#*/}; \
			OUT_NAME="$(BINARY)_$(VERSION)_$${GOOS}_$${GOARCH}"; \
			OUT_DIR="$(DIST_DIR)/$${OUT_NAME}"; \
			mkdir -p "$$OUT_DIR"; \
			GOOS=$$GOOS GOARCH=$$GOARCH CGO_ENABLED=0 go build -ldflags "-X main.Version=$(VERSION)" -o "$$OUT_DIR/$(BINARY)" $(CMD); \
			tar -C "$$OUT_DIR" -czf "$(DIST_DIR)/$${OUT_NAME}.tar.gz" $(BINARY); \
			rm -rf "$$OUT_DIR"; \
	done
	cd $(DIST_DIR) && sha256sum *.tar.gz > checksums.txt
