NEDGE_DEST = $(DESTDIR)/opt/nedge/sbin
NEDGE_ETC = $(DESTDIR)/opt/nedge/etc/ccow
NDNFS_EXE = ndnfs

GIT_REPOSITORY = github.com/Nexenta/${PLUGIN_NAME}

GIT_BRANCH = $(shell git rev-parse --abbrev-ref HEAD | sed -e "s/.*\\///")
VERSION ?= ${GIT_BRANCH}
COMMIT ?= $(shell git rev-parse HEAD | cut -c 1-7)
DATETIME ?= $(shell date -u +'%F_%T')
LDFLAGS ?= \
	-X ${GIT_REPOSITORY}/pkg/config.Version=${VERSION} \
	-X ${GIT_REPOSITORY}/pkg/config.Commit=${COMMIT} \
	-X ${GIT_REPOSITORY}/pkg/config.DateTime=${DATETIME}

ifeq ($(GOPATH),)
GOPATH = $(shell pwd)
endif

build:

	env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/${NDNFS_EXE} -ldflags "${LDFLAGS}" ./ndnfs/ndnfs.go
lint:
	go get -v github.com/golang/lint/golint
	for file in $$(find $(GOPATH)/src/github.com/Nexenta/nedge-docker-nfs -name '*.go' | grep -v vendor | grep -v '\.pb\.go' | grep -v '\.pb\.gw\.go'); do \
		$(GOPATH)/bin/golint $${file}; \
		if [ -n "$$($(GOPATH)/bin/golint $${file})" ]; then \
			exit 1; \
		fi; \
	done

install:
	cp -n $(GOPATH)/src/github.com/Nexenta/nedge-docker-nfs/ndnfs/daemon/ndnfs.json $(NEDGE_ETC)/ndnfs.json.example
	cp -f $(GOPATH)/bin/$(NDNFS_EXE) $(NEDGE_DEST)

uninstall:
	rm -f $(NEDGE_ETC)/ndnfs.json
	rm -f $(NEDGE_DEST)/ndnfs
