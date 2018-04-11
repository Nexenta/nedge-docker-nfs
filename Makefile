NEDGE_DEST = $(DESTDIR)/opt/nedge/sbin
NEDGE_ETC = $(DESTDIR)/opt/nedge/etc/ccow
NDNFS_EXE = ndnfs

ifeq ($(GOPATH),)
	GOPATH = $(shell pwd)
endif

build:
	GOPATH=$(GOPATH) go build -o $(GOPATH)/bin/$(NDNFS_EXE) ./ndnfs/ndnfs.go

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
