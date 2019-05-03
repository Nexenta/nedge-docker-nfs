PLUGIN_NAME = nedge-docker-nfs
PLUGIN_EXECUTABLE_NAME = ndnfs
IMAGE_NAME ?= ${PLUGIN_NAME}

GIT_REPOSITORY = github.com/Nexenta/${PLUGIN_NAME}

GIT_BRANCH = $(shell git rev-parse --abbrev-ref HEAD | sed -e "s/.*\\///")
VERSION ?= ${GIT_BRANCH}
COMMIT ?= $(shell git rev-parse HEAD | cut -c 1-7)
DATETIME ?= $(shell date -u +'%F_%T')
LDFLAGS ?= \
	-X ${GIT_REPOSITORY}/pkg/config.Version=${VERSION} \
	-X ${GIT_REPOSITORY}/pkg/config.Commit=${COMMIT} \
	-X ${GIT_REPOSITORY}/pkg/config.DateTime=${DATETIME}

build:
	env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/${PLUGIN_EXECUTABLE_NAME} -ldflags "${LDFLAGS}" ./ndnfs/ndnfs.go
lint:
	go get -v github.com/golang/lint/golint
	for file in $$(find $(GOPATH)/src/github.com/Nexenta/nedge-docker-nfs -name '*.go' | grep -v vendor | grep -v '\.pb\.go' | grep -v '\.pb\.gw\.go'); do \
		$(GOPATH)/bin/golint $${file}; \
		if [ -n "$$($(GOPATH)/bin/golint $${file})" ]; then \
			exit 1; \
		fi; \
	done
