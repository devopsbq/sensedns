package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/hashicorp/consul/api"
	"github.com/miekg/dns"
)

func (s *SenseDNS) fillWithData(pairs api.KVPairs, network string) {
	zs := s.dnsServer.zones
	zs.Lock()
	defer zs.Unlock()
	key := dns.Fqdn(network + ".sx.")
	zs.store[key] = make(map[dns.RR_Header][]dns.RR)
	for _, value := range pairs {
		path := strings.Split(value.Key, "/")
		hostname := fmt.Sprintf("%s.%s.sx", path[3], network)
		ip := string(value.Value)
		rr := new(dns.A)
		rr.A = net.ParseIP(ip)
		rr.Hdr = dns.RR_Header{Name: dns.Fqdn(hostname), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600}
		key2 := dns.RR_Header{Name: dns.Fqdn(rr.Header().Name), Rrtype: rr.Header().Rrtype, Class: rr.Header().Class}
		zs.store[key][key2] = append(zs.store[key][key2], rr)
	}
}
