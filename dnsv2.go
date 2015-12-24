package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/miekg/dns"
)

// DNSServer expose service discovery endpoints.
type DNSServer struct {
	dnsMux       *dns.ServeMux
	dnsServerUDP *dns.Server
	dnsServerTCP *dns.Server
	recursors    []string
	networkTLD   string
}

// NewDNSServer returns a new DNS server
func NewDNSServer(addr, networkTLD string, recursors []string) (*DNSServer, error) {
	networkTLD = dns.Fqdn(networkTLD)
	mux := dns.NewServeMux()

	serverUDP := &dns.Server{Addr: addr, Net: "udp", Handler: mux, UDPSize: 65535}
	serverTCP := &dns.Server{Addr: addr, Net: "tcp", Handler: mux}
	// TODO include notify started function on dnsservers

	server := &DNSServer{
		dnsMux:       mux,
		dnsServerUDP: serverUDP,
		dnsServerTCP: serverTCP,
		recursors:    recursors,
		networkTLD:   networkTLD,
		// TODO zoneStore ?
	}

	mux.HandleFunc(dns.Fqdn("arpa"), server.reverse)
	mux.HandleFunc(networkTLD, server.resolve)
	if len(server.recursors) > 0 {
		for idx, recursor := range server.recursors {
			recursor, err := recursorAddr(recursor)
			if err != nil {
				return nil, fmt.Errorf("Invalid recursor address: %v", err)
			}
			server.recursors[idx] = recursor.String()
		}
		mux.HandleFunc(".", server.recurse)
	}
	return server, nil
}

// ListenAndServe starts the DNS server.
func (s *DNSServer) ListenAndServe() error {
	go start(s.dnsServerUDP)
	go start(s.dnsServerTCP)
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case sr := <-sig:
			log.Infof("Signal (%d) received, stopping", sr)
			return s.Shutdown()
		}
	}
}

// Shutdown stops the DNS server.
func (s *DNSServer) Shutdown() error {
	var err error
	if err = s.dnsServerUDP.Shutdown(); err != nil {
		// TODO: log
	}
	if err = s.dnsServerTCP.Shutdown(); err != nil {
		// TODO: log
	}
	// TODO: think about cleaning something!
	return err
}

func (s *DNSServer) reverse(w dns.ResponseWriter, r *dns.Msg) {
	// TODO log (time?)
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.RecursionAvailable = (len(s.recursors) > 0)

	// TODO: SOA?
	// TODO: resolve

	if len(m.Answer) == 0 {
		s.recurse(w, r)
		return
	}
	respond(w, m)
}

func (s *DNSServer) resolve(w dns.ResponseWriter, r *dns.Msg) {
	// TODO log (time?)
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.RecursionAvailable = (len(s.recursors) > 0)

	// network := getNetworkType(w.RemoteAddr())
	// TODO: SOA?
	// TODO: resolve

	respond(w, m)
}

func (s *DNSServer) recurse(w dns.ResponseWriter, r *dns.Msg) {
	// TODO log (time?)
	c := &dns.Client{Net: getNetworkType(w.RemoteAddr())}
	for _, recursor := range s.recursors {
		if r, _, err := c.Exchange(r, recursor); err == nil {
			respond(w, r)
			return
		}
		// TODO: log error
	}
	// TODO: log
	m := new(dns.Msg)
	m.SetReply(r)
	m.RecursionAvailable = true
	m.SetRcode(r, dns.RcodeServerFailure)
	respond(w, m)
}

func start(ds *dns.Server) {
	log.Infof("Starting %s listener on %s", ds.Net, ds.Addr)
	if err := ds.ListenAndServe(); err != nil {
		log.Fatalf("Start %s listener on %s failed: %s", ds.Net, ds.Addr, err.Error())
		// TODO: think about this (should be fatal?)
	}
}

func recursorAddr(recursor string) (net.Addr, error) {
	for {
		if _, _, err := net.SplitHostPort(recursor); err != nil {
			if ae, ok := err.(*net.AddrError); ok && ae.Err == "missing port in address" {
				recursor = fmt.Sprintf("%s:%d", recursor, 53)
				continue
			}
			return nil, err
		}
		return net.ResolveTCPAddr("tcp", recursor) // TODO why TCP?
	}
}

func getNetworkType(addr net.Addr) string {
	if _, ok := addr.(*net.TCPAddr); ok {
		return "tcp"
	}
	return "udp"
}

func respond(w dns.ResponseWriter, r *dns.Msg) {
	if err := w.WriteMsg(r); err != nil {
		log.Warnf("failed to respond: %v", err)
	}
}

// Example request
// {
// 	"Id": 10298,
// 	"Response": false,
// 	"Opcode": 0,
// 	"Authoritative": false,
// 	"Truncated": false,
// 	"RecursionDesired": true,
// 	"RecursionAvailable": false,
// 	"Zero": false,
// 	"AuthenticatedData": false,
// 	"CheckingDisabled": false,
// 	"Rcode": 0,
// 	"Question": [
// 		{
// 			"Name": "prueba.prod.sensends.",
// 			"Qtype": 1,
// 			"Qclass": 1
// 		}
// 	],
// 	"Answer": [],
// 	"Ns": [],
// 	"Extra": []
// }
