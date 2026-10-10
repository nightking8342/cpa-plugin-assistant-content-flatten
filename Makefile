# Build the assistant-content-flatten CLIProxyAPI plugin.
#
# CPA loads plugins as in-process dynamic libraries, so the build uses
# -buildmode=c-shared and therefore needs a C compiler (gcc/clang) for the
# target platform. `make test` and `make vet` do not.

PLUGIN_ID := assistant-content-flatten
GO        ?= go
GOOS      ?= $(shell $(GO) env GOOS)
GOARCH    ?= $(shell $(GO) env GOARCH)
VERSION   ?=
BUILD_DIR ?= dist/$(GOOS)/$(GOARCH)

ifeq ($(GOOS),windows)
EXT := .dll
else ifeq ($(GOOS),darwin)
EXT := .dylib
else
EXT := .so
endif

# Optional "-v<version>" suffix. The host derives the plugin id and version from
# the file name, so `assistant-content-flatten-v0.1.0.so` registers as plugin
# "assistant-content-flatten" version "0.1.0".
NAME := $(PLUGIN_ID)$(if $(VERSION),-v$(VERSION),)

.PHONY: all build test vet fmt clean docker-linux-amd64 docker-linux-arm64

all: build

build:
	CGO_ENABLED=1 $(GO) build -trimpath -buildmode=c-shared -ldflags "-s -w" \
		-o $(BUILD_DIR)/$(NAME)$(EXT) .

test:
	CGO_ENABLED=0 $(GO) test ./...

vet:
	CGO_ENABLED=0 $(GO) vet ./...

fmt:
	$(GO) fmt ./...

clean:
	rm -rf dist

# Cross-build a Linux .so without a local Linux toolchain. Use the glibc-based
# (bookworm) image: CPA's runtime is Debian-based, and an alpine/musl build fails
# to dlopen there.
docker-linux-amd64:
	docker run --rm -v "$(CURDIR)":/src -w /src -e CGO_ENABLED=1 golang:1.24-bookworm \
		go build -trimpath -buildmode=c-shared -ldflags "-s -w" \
		-o dist/linux/amd64/$(NAME).so .

docker-linux-arm64:
	docker run --rm --platform linux/arm64 -v "$(CURDIR)":/src -w /src -e CGO_ENABLED=1 golang:1.24-bookworm \
		go build -trimpath -buildmode=c-shared -ldflags "-s -w" \
		-o dist/linux/arm64/$(NAME).so .
