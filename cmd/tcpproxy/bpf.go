package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
)

// ProxyDispatchBPF provides interaction with a socket steering BPF program
// loaded into the kernel.
type ProxyDispatchBPF struct {
	objs      *pdBPFObjs
	netnsLink *link.NetNsLink
}

type pdBPFObjs struct {
	Prog   *ebpf.Program `ebpf:"proxy_dispatch"`
	Socket *ebpf.Map     `ebpf:"proxy_socket"`
	Ports  *ebpf.Map     `ebpf:"proxy_ports"`
}

func (objs *pdBPFObjs) Close() error {
	return closeAll([]io.Closer{
		objs.Prog,
		objs.Socket,
		objs.Ports,
	})
}

var maxPorts = 1024
var bpfELF = "./proxy_dispatch.o"

// InitProxyDispatchBPF takes the underlying file descriptor of a listener to
// which connections arriving on any of the given ports should be routed.
//
// It reads a BPF program ELF file into memory and loads the program along with
// Port and Socket BPF maps into the kernel. The BPF maps are used to
// communicate between the running Go program in userspace and the running BPF
// program in kernelspace. The file descriptor sockfd is put into the single
// entry Socket map at key 0 and each port is put into the Port map with a value
// of 0. Finally, the program is attached to a network namespace.
func InitProxyDispatchBPF(sockfd uintptr, ports ...int) (*ProxyDispatchBPF, error) {
	if len(ports) > maxPorts {
		return nil, fmt.Errorf("number of ports exceeds limit of %d", maxPorts)
	}

	elfBytes, err := ioutil.ReadFile(bpfELF)
	if err != nil {
		return nil, fmt.Errorf("loading elf: %w", err)
	}

	// Allow the current process to lock memory for eBPF resources.
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("removing memlock: %w", err)
	}

	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(elfBytes[:]))
	if err != nil {
		return nil, fmt.Errorf("loading collection spec: %w", err)
	}

	// Load the program and maps into the kernel and assign them to a struct.
	var objs pdBPFObjs
	if err := spec.LoadAndAssign(&objs, nil); err != nil {
		return nil, fmt.Errorf("loading and assigning objs: %w", err)
	}

	err = objs.Socket.Put(uint32(0), uint64(sockfd))
	if err != nil {
		objs.Close()
		return nil, fmt.Errorf("putting socket: %w", err)
	}

	for _, port := range ports {
		log.Printf("INFO: routing connections on :%d to listener", port)
		err := objs.Ports.Put(uint16(port), uint8(0))
		if err != nil {
			objs.Close()
			return nil, fmt.Errorf("putting port %d in map: %w", port, err)
		}
	}

	// Attach the program to a network namespace.
	netnsLink, err := attachNetNs(objs.Prog)
	if err != nil {
		objs.Close()
		return nil, fmt.Errorf("attaching program: %w", err)
	}

	return &ProxyDispatchBPF{
		objs:      &objs,
		netnsLink: netnsLink,
	}, nil
}

var netnsPath = "/proc/self/ns/net"

func attachNetNs(prog *ebpf.Program) (*link.NetNsLink, error) {
	netns, err := os.Open(netnsPath)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", netnsPath, err)
	}
	defer netns.Close()
	netnsLink, err := link.AttachNetNs(int(netns.Fd()), prog)
	if err != nil {
		return nil, fmt.Errorf("attaching program to network namespace: %w", err)
	}
	return netnsLink, nil
}

// Close unloads the BPF program from the kernel, removes the maps, and breaks
// the link between the program and the network namespace.
func (pdbpf *ProxyDispatchBPF) Close() error {
	return closeAll([]io.Closer{
		pdbpf.objs,
		pdbpf.netnsLink,
	})
}

func closeAll(s []io.Closer) error {
	errs := errSlice{}
	for _, c := range s {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}
