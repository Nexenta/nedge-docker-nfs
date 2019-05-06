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
	# docker/go-plugins-helpers
	GOPATH=$(GOPATH) go get -d -v github.com/docker/go-plugins-helpers/volume
	cd $(GOPATH)/src/github.com/docker/go-plugins-helpers; git checkout d7fc7d0
	# opencontainers/runc
	GOPATH=$(GOPATH) go get -d -v github.com/opencontainers/runc
	cd $(GOPATH)/src/github.com/opencontainers/runc; git checkout aada2af
	# docker/go-connections
	GOPATH=$(GOPATH) go get -d -v github.com/docker/go-connections
	cd $(GOPATH)/src/github.com/docker/go-connections; git checkout acbe915
	# NDNFS
	GOPATH=$(GOPATH) go get -d github.com/Nexenta/nedge-docker-nfs/...
	cd $(GOPATH)/src/github.com/Nexenta/nedge-docker-nfs; git checkout stable/v13
	GOPATH=$(GOPATH) go get github.com/Nexenta/nedge-docker-nfs/...

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
