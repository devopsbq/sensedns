package main

import (
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/miekg/dns"
)

var (
	timeout       string
	err           error
	consulURL     string
	consulTimeout time.Duration
)

type ZoneStore struct {
	store map[string]Zone
	sync.RWMutex
}

type Zone map[dns.RR_Header][]dns.RR

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
			} else {
				// Continue for DS to see if we have a parent too, if so delegate to the parent
				zone = &z
				name = string(b[:l])
			}
		}
		off, end = dns.NextLabel(q, off)
		if end {
			break
		}
	}
	return zone, name
}

type Server struct {
	host      string
	port      int
	recurseTo string
	rTimeout  time.Duration
	wTimeout  time.Duration
	zones     *ZoneStore
}

func (s *Server) Addr() string {
	return s.host + ":" + strconv.Itoa(s.port)
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
	log.Printf("Start %s listener on %s\n", ds.Net, s.Addr())
	err := ds.ListenAndServe()
	if err != nil {
		log.Fatalf("Start %s listener on %s failed:%s", ds.Net, s.Addr(), err.Error())
	}
}
