package main

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
	"github.com/miekg/dns"
)

type ZoneStore struct {
	store map[string]Zone
	sync.RWMutex
}

type Zone map[dns.RR_Header][]dns.RR

type Server struct {
	host       string
	port       string
	recurseTo  string
	networkTLD string
	rTimeout   time.Duration
	wTimeout   time.Duration
	zones      *ZoneStore
}

func newDNS() *Server {
	return &Server{
		rTimeout: 5 * time.Second,
		wTimeout: 5 * time.Second,
		zones: &ZoneStore{
			store: make(map[string]Zone),
		},
	}
}

func (zs *ZoneStore) match(q string, t uint16) (*Zone, string) {
	zs.RLock()
	defer zs.RUnlock()
	var zone *Zone
	var name string
	b := make([]byte, len(q)) // worst case, one label of length q
	off := 0
	end := false
	for {
		l := len(q[off:])
		for i := 0; i < l; i++ {
			b[i] = q[off+i]
			if b[i] >= 'A' && b[i] <= 'Z' {
				b[i] |= ('a' - 'A')
			}
		}
		if z, ok := zs.store[string(b[:l])]; ok { // 'causes garbage, might want to change the map key
			if t != dns.TypeDS {
				return &z, string(b[:l])
			}
			// Continue for DS to see if we have a parent too, if so delegate to the parent
			zone = &z
			name = string(b[:l])
		}
		off, end = dns.NextLabel(q, off)
		if end {
			break
		}
	}
	return zone, name
}

func (s *Server) Addr() string {
	return s.host + ":" + s.port
}

func (s *Server) Run() {
	tcpHandler := dns.NewServeMux()
	tcpHandler.HandleFunc(".", s.DoTCP)
	udpHandler := dns.NewServeMux()
	udpHandler.HandleFunc(".", s.DoUDP)

	tcpServer := &dns.Server{Addr: s.Addr(),
		Net: "tcp", Handler: tcpHandler,
		ReadTimeout: s.rTimeout, WriteTimeout: s.wTimeout,
	}

	udpServer := &dns.Server{Addr: s.Addr(),
		Net: "udp", Handler: udpHandler,
		UDPSize:     65535,
		ReadTimeout: s.rTimeout, WriteTimeout: s.wTimeout,
	}

	go s.start(udpServer)
	go s.start(tcpServer)
}

func (s *Server) start(ds *dns.Server) {
	log.Infof("Start %s listener on %s", ds.Net, s.Addr())
	err := ds.ListenAndServe()
	if err != nil {
		log.Fatalf("Start %s listener on %s failed: %s", ds.Net, s.Addr(), err.Error())
	}
}

func (s *SenseDNS) fillWithData(pairs api.KVPairs, network string) {
	zs := s.dnsServer.zones
	zs.Lock()
	defer zs.Unlock()
	key := dns.Fqdn(fmt.Sprintf("%s.%s.", network, s.dnsServer.networkTLD))
	zs.store[key] = make(map[dns.RR_Header][]dns.RR)
	for _, value := range pairs {
		path := strings.Split(value.Key, "/")
		hostname := fmt.Sprintf("%s.%s.%s", path[3], network, s.dnsServer.networkTLD)
		ip := string(value.Value)
		rr := new(dns.A)
		rr.A = net.ParseIP(ip)
		rr.Hdr = dns.RR_Header{Name: dns.Fqdn(hostname), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600}
		key2 := dns.RR_Header{Name: dns.Fqdn(rr.Header().Name), Rrtype: rr.Header().Rrtype, Class: rr.Header().Class}
		zs.store[key][key2] = append(zs.store[key][key2], rr)
	}
	if log.Level == logrus.DebugLevel {
		s.dnsServer.printRoutingTable()
	}
}

func (s *Server) roundRobin(zone *Zone, address dns.RR_Header) {
	log.WithField("domain", address.Name).Debug("round-robin records")
	hosts := (*zone)[address]
	if len(hosts) > 1 {
		for i := 0; i < len(hosts)-1; i++ {
			hosts[i], hosts[i+1] = hosts[i+1], hosts[i]
		}
	}
}

func (s *Server) printRoutingTable() {
	var buffer bytes.Buffer
	for k, zone := range s.zones.store {
		if len(zone) > 0 {
			buffer.WriteString(fmt.Sprintf("-- %s\n", k))
		}
		for key, z := range zone {
			buffer.WriteString(fmt.Sprintf("---- %s\n", key.String()))
			for _, value := range z {
				buffer.WriteString(fmt.Sprintf("------ %s\n", value.String()))
			}
		}
	}
	if buffer.Len() > 0 {
		log.Debugf("routing table: \n%s", strings.TrimRight(buffer.String(), "\n"))
	}
}
