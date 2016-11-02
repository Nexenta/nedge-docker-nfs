NEDGE_DEST = $(DESTDIR)/opt/nedge/sbin
NEDGE_ETC = $(DESTDIR)/opt/nedge/etc/ccow
NDVOL_EXE = ndnfs

build: 
	GOPATH=$(shell pwd) go get -v github.com/docker/go-plugins-helpers/...
	cd src/github.com/docker/go-plugins-helpers/volume; git checkout 60d242c
	GOPATH=$(shell pwd) go get -v github.com/Nexenta/nedge-docker-nfs/...

lint:
	GOPATH=$(shell pwd) GOROOT=$(GO_INSTALL) $(GO) get -v github.com/golang/lint/golint
	for file in $$(find . -name '*.go' | grep -v vendor | grep -v '\.pb\.go' | grep -v '\.pb\.gw\.go'); do \
		golint $${file}; \
		if [ -n "$$(golint $${file})" ]; then \
			exit 1; \
		fi; \
	done

install:
	cp -f bin/$(NDNFS_EXE) $(NEDGE_DEST)

uninstall:
	rm -f $(NEDGE_ETC)/ndnfs.json
	rm -f $(NEDGE_DEST)/ndnfs

clean:
	go clean github.com/Nexenta/nedge-docker-nfs
