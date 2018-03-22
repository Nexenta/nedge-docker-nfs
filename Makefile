build:
	go get -d -v github.com/opencontainers/runc
	cd $(GOPATH)/src/github.com/opencontainers/runc; git checkout aada2af
	go get -v github.com/docker/go-plugins-helpers/volume
	cd $(GOPATH)/src/github.com/docker/go-plugins-helpers/volume; git checkout d7fc7d0
	cd $(GOPATH)/src/github.com/docker/go-connections; git checkout acbe915
	go get -v github.com/Nexenta/nedge-docker-nfs/...

lint:
	go get -v github.com/golang/lint/golint
	for file in $$(find $(GOPATH)/src/github.com/Nexenta/nedge-docker-nfs -name '*.go' | grep -v vendor | grep -v '\.pb\.go' | grep -v '\.pb\.gw\.go'); do \
		$(GOPATH)/bin/golint $${file}; \
		if [ -n "$$($(GOPATH)/bin/golint $${file})" ]; then \
			exit 1; \
		fi; \
	done

