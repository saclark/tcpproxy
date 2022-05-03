//go:build ignore
// +build ignore

// SPDX-License-Identifier: (GPL-2.0-only OR BSD-2-Clause)
/* Copyright (c) 2020 Cloudflare */
/*
 * BPF socket lookup program that dispatches connections destined to a
 * configured set of open ports, to the proxy service socket.
 *
 * Program expects the proxy service socket to be in the `proxy_socket` BPF map.
 * Port is considered open when an entry for that port number exists in the
 * `proxy_ports` BPF hashmap.
 *
 */

#include <linux/bpf.h>

#include <bpf/bpf_endian.h>
#include <bpf/bpf_helpers.h>

/* Declare BPF maps */

/* List of open proxy service ports. Key is the port number. */
struct bpf_map_def SEC("maps") proxy_ports = {
    .type        = BPF_MAP_TYPE_HASH,
    .max_entries = 1024,
    .key_size    = sizeof(__u16),
    .value_size  = sizeof(__u8),
};

/* Proxy server socket */
struct bpf_map_def SEC("maps") proxy_socket = {
    .type        = BPF_MAP_TYPE_SOCKMAP,
    .max_entries = 1,
    .key_size    = sizeof(__u32),
    .value_size  = sizeof(__u64),
};

/* Dispatcher program for the proxy service */
SEC("sk_lookup/proxy_dispatch")
int proxy_dispatch(struct bpf_sk_lookup *ctx)
{
    const __u32 zero = 0;
    struct bpf_sock *sk;
    __u16 port;
    __u8 *open;
    long err;

    /* Is proxy service enabled on packets destination port? */
    port = ctx->local_port;
    open = bpf_map_lookup_elem(&proxy_ports, &port);
    if (!open)
        return SK_PASS;

    /* Get proxy server socket */
    sk = bpf_map_lookup_elem(&proxy_socket, &zero);
    if (!sk)
    return SK_DROP;

    /* Dispatch the packet to proxy server socket */
    err = bpf_sk_assign(ctx, sk, 0);
    bpf_sk_release(sk);
    return err ? SK_DROP : SK_PASS;
    return SK_PASS;
}

SEC("license") const char __license[] = "Dual BSD/GPL";
