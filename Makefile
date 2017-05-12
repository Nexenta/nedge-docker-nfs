NEDGE_DEST = $(DESTDIR)/opt/nedge/sbin
NEDGE_ETC = $(DESTDIR)/opt/nedge/etc/ccow
NDNFS_EXE = ndnfs

build:
	GOPATH=$(shell pwd) go get -v github.com/docker/go-plugins-helpers/volume
	cd src/github.com/docker/go-plugins-helpers/volume; git checkout d7fc7d0
	cd src/github.com/docker/go-connections; git checkout acbe915
	GOPATH=$(shell pwd) go get -v github.com/nexenta/nedge-docker-nfs/...

lint:
	GOPATH=$(shell pwd) GOROOT=$(GO_INSTALL) $(GO) get -v github.com/golang/lint/golint
	for file in $$(find . -name '*.go' | grep -v vendor | grep -v '\.pb\.go' | grep -v '\.pb\.gw\.go'); do \
		golint $${file}; \
		if [ -n "$$(golint $${file})" ]; then \
			exit 1; \
		fi; \
	done

install:
	cp -n ndnfs/daemon/ndnfs.json $(NEDGE_ETC)/ndnfs.json.example
	cp -f bin/$(NDNFS_EXE) $(NEDGE_DEST)

uninstall:
	rm -f $(NEDGE_ETC)/ndnfs.json
	rm -f $(NEDGE_DEST)/ndnfs

clean:
	go clean github.com/nexenta/nedge-docker-nfs
