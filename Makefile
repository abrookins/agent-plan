APP := agent-plan
BIN_DIR := dist
PKG := ./cmd/agent-plan
TARGETS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64
HOST_OS := $(shell go env GOOS)
HOST_ARCH := $(shell go env GOARCH)
HOST_EXT := $(if $(filter windows,$(HOST_OS)),.exe,)
HOST_BIN := $(BIN_DIR)/$(APP)-$(HOST_OS)-$(HOST_ARCH)$(HOST_EXT)

.PHONY: build build-all test install clean

build:
	mkdir -p $(BIN_DIR)
	go build -o $(HOST_BIN) $(PKG)

build-all:
	mkdir -p $(BIN_DIR)
	@for target in $(TARGETS); do \
		os=$${target%/*}; \
		arch=$${target#*/}; \
		out="$(BIN_DIR)/$(APP)-$$os-$$arch"; \
		if [ "$$os" = "windows" ]; then out="$$out.exe"; fi; \
		echo "building $$out"; \
		GOOS=$$os GOARCH=$$arch go build -o "$$out" $(PKG) || exit 1; \
	done

test:
	go test ./...

install:
	go install $(PKG)

clean:
	rm -rf $(BIN_DIR)
