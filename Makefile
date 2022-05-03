CC=clang
CFLAGS=-O2 -target bpf

.PHONY: all
all: artifacts artifacts/config.json artifacts/proxy_dispatch.o artifacts/tcpproxy

artifacts/tcpproxy: artifacts cmd/tcpproxy
	go build -o $(@) github.com/saclark/tcpproxy/cmd/tcpproxy

artifacts/proxy_dispatch.o: artifacts cmd/tcpproxy/proxy_dispatch.c
	$(CC) -I/usr/local/include $(CFLAGS) -c cmd/tcpproxy/proxy_dispatch.c -o $(@)

artifacts/config.json: artifacts config.json
	cp config.json $(@)

artifacts: clean
	mkdir -p $(@)

.PHONY: clean
clean:
	rm -rf artifacts
