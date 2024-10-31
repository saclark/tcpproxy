# tcpproxy

A raw TCP proxy capable of load balancing connections on a configured set of ports to a configured set of backends. It features an optional eBPF socket lookup hook to steer incoming connections on any of the configured ports to a single listener socket, based on [this talk](https://www.youtube.com/watch?v=vCJ8kDYI8ZE).

> **Note**: This is a prototype created for hoots and toots and as a way to play with eBPF. It only does round robin load balancing, does not support live configuration reload, and unhealthy targets are not removed from the available target pool with background health checks. Such things were not the point or focus of this project.

## Build

```
go build github.com/saclark/tcpproxy/cmd/tcpproxy
```

## Run

To run the proxy without socket steering and bind a listener to each configured port:

```bash
./tcpproxy
```

To run the proxy with socket steering and bind a single listener to a single port (note that this requires a Linux host):

```bash
./tcpproxy -port 4001
```

## Develop

You'll need the following to run and test the eBPF code:

* A Linux host running kernel >= 5.9
* `libbpf` source code
* `clang` > 10

With those dependencies in place, run `make` from the root directory of this repo and you'll find the generated artifacts in the `artifacts` directory.

This repo contains source for a VS Code remote Linux dev server with those dependencies installed. To run it on [fly.io](https://fly.io):

```
flyctl launch
flyctl volumes create data
flyctl secrets set ROOT_PASSWORD=<somethingsecret>
flyctl deploy
```

Once your VM is deployed, you should be able to access it at `ssh://root:<somethingsecret>@yourapp.fly.dev:2222`. Follow [these docs](https://code.visualstudio.com/docs/remote/ssh) to connect to it from VS Code on your machine and pull down your code from GitHub.

