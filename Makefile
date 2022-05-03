CC=clang
CFLAGS=-O2 -target bpf

.PHONY: all
all: artifacts artifacts/config.json artifacts/tcpproxy

artifacts/tcpproxy: artifacts cmd/tcpproxy
	go build -o $(@) github.com/saclark/tcpproxy/cmd/tcpproxy

artifacts/config.json: artifacts config.json
	cp config.json $(@)

artifacts: clean
	mkdir -p $(@)

.PHONY: clean
clean:
	rm -rf artifacts
