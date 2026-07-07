// SPDX-License-Identifier: MIT
#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/pkt_cls.h>
#include <linux/udp.h>

#define SEC(name) __attribute__((section(name), used))
#define __uint(name, val) int (*name)[val]
#define __type(name, val) typeof(val) *name

#ifndef BPF_ANY
#define BPF_ANY 0
#endif

#define TS_PACKET_SIZE 188
#define TS_SYNC_BYTE 0x47
/* Normal IPTV over 1500 MTU carries up to 7 TS packets (1316 bytes).
 * Larger jumbo-frame UDP payloads are intentionally left for a later benchmark.
 */
#define MAX_TS_PACKETS_PER_UDP 8
/* Standard 1500 MTU fits 7 TS packets (1316 bytes). Increase for jumbo frames. */
#define IPPROTO_UDP 17

static void *(*bpf_map_lookup_elem)(void *map, const void *key) = (void *)1;
static long (*bpf_map_update_elem)(void *map, const void *key, const void *value, unsigned long long flags) = (void *)2;

struct stream_pid_key {
	__u32 dst_ip;
	__u16 dst_port;
	__u16 pid;
};

struct pid_stats_value {
	__u64 packets;
	__u64 drops;
	__u64 duplicates;
	__u64 tei_errors;
	__u64 discontinuities;
	__u64 sync_losses;
	__u8 last_cc;
	__u8 seen;
	__u8 reserved0;
	__u8 reserved1;
};

struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, 65536);
	__type(key, struct stream_pid_key);
	__type(value, struct pid_stats_value);
} pid_stats SEC(".maps");

static __always_inline void process_ts(unsigned char *p, __u32 dst_ip, __u16 dst_port)
{
	struct stream_pid_key key = {
		.dst_ip = dst_ip,
		.dst_port = dst_port,
		.pid = ((__u16)(p[1] & 0x1f) << 8) | p[2],
	};

	__u8 tei = p[1] & 0x80;
	__u8 afc = (p[3] >> 4) & 0x03;
	__u8 cc = p[3] & 0x0f;
	__u8 has_adaptation = (afc == 2) || (afc == 3);
	__u8 has_payload = (afc == 1) || (afc == 3);
	__u8 discontinuity = 0;

	if (afc == 0)
		return;

	if (has_adaptation) {
		__u8 af_len = p[4];
		if (af_len > 0)
			discontinuity = p[5] & 0x80;
	}

	struct pid_stats_value *stats = bpf_map_lookup_elem(&pid_stats, &key);
	if (!stats) {
		struct pid_stats_value init = {};
		bpf_map_update_elem(&pid_stats, &key, &init, BPF_ANY);
		stats = bpf_map_lookup_elem(&pid_stats, &key);
		if (!stats)
			return;
	}

	__sync_fetch_and_add(&stats->packets, 1);

	if (tei)
		__sync_fetch_and_add(&stats->tei_errors, 1);

	if (discontinuity) {
		__sync_fetch_and_add(&stats->discontinuities, 1);
		stats->seen = 0;
		return;
	}

	if (!has_payload)
		return;

	if (!stats->seen) {
		stats->last_cc = cc;
		stats->seen = 1;
		return;
	}

	__u8 expected = (stats->last_cc + 1) & 0x0f;
	if (cc == expected) {
		stats->last_cc = cc;
	} else if (cc == stats->last_cc) {
		__sync_fetch_and_add(&stats->duplicates, 1);
	} else {
		__u8 missing = (cc - expected) & 0x0f;
		if (missing == 0)
			missing = 1;
		__sync_fetch_and_add(&stats->drops, missing);
		stats->last_cc = cc;
	}
}

SEC("tc")
int tc_mpeg2ts(struct __sk_buff *skb)
{
	void *data = (void *)(long)skb->data;
	void *data_end = (void *)(long)skb->data_end;

	struct ethhdr *eth = data;
	if ((void *)(eth + 1) > data_end)
		return TC_ACT_OK;
	if (eth->h_proto != __constant_htons(ETH_P_IP))
		return TC_ACT_OK;

	struct iphdr *iph = (void *)(eth + 1);
	if ((void *)(iph + 1) > data_end)
		return TC_ACT_OK;
	if (iph->protocol != IPPROTO_UDP)
		return TC_ACT_OK;

	__u32 ihl = iph->ihl * 4;
	if (ihl < sizeof(*iph))
		return TC_ACT_OK;
	if ((void *)iph + ihl > data_end)
		return TC_ACT_OK;

	struct udphdr *udph = (void *)iph + ihl;
	if ((void *)(udph + 1) > data_end)
		return TC_ACT_OK;

	unsigned char *payload = (void *)(udph + 1);
	__u16 udp_len = __builtin_bswap16(udph->len);
	if (udp_len < sizeof(*udph))
		return TC_ACT_OK;
	__u32 payload_len = udp_len - sizeof(*udph);
	if (payload_len > TS_PACKET_SIZE * MAX_TS_PACKETS_PER_UDP)
		payload_len = TS_PACKET_SIZE * MAX_TS_PACKETS_PER_UDP;

#pragma unroll
	for (int i = 0; i < MAX_TS_PACKETS_PER_UDP; i++) {
		if ((i + 1) * TS_PACKET_SIZE > payload_len)
			break;

		unsigned char *pkt = payload + (i * TS_PACKET_SIZE);

		/* Explicit bounds check for verifier */
		if (pkt + 6 > data_end)
			break;
		if (pkt[0] != TS_SYNC_BYTE) {
			struct stream_pid_key sync_key = {
				.dst_ip = iph->daddr,
				.dst_port = udph->dest,
				.pid = 0xffff,
			};
			struct pid_stats_value *ss = bpf_map_lookup_elem(&pid_stats, &sync_key);
			if (!ss) {
				struct pid_stats_value init = {};
				bpf_map_update_elem(&pid_stats, &sync_key, &init, BPF_ANY);
				ss = bpf_map_lookup_elem(&pid_stats, &sync_key);
			}
			if (ss)
				__sync_fetch_and_add(&ss->sync_losses, 1);
			continue;
		}

		/* Verify full packet fits before data_end */
		if (pkt + TS_PACKET_SIZE > data_end)
			break;

		process_ts(pkt, iph->daddr, udph->dest);
	}

	return TC_ACT_OK;
}

char _license[] SEC("license") = "MIT";
