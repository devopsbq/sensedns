package main

import "github.com/miekg/dns"

func (s *Server) recurse(w dns.ResponseWriter, req *dns.Msg) {
	if s.recurseTo == "" {
		dns.HandleFailed(w, req)
		return
	}
	c := new(dns.Client)
	in, _, err := c.Exchange(req, s.recurseTo)
	if err == nil {
		if in.MsgHdr.Truncated {
			c.Net = "tcp"
			in, _, err = c.Exchange(req, s.recurseTo)
		}
		w.WriteMsg(in)
		return
	}
	log.Warnf("Recursive error: %+v\n", err)
	dns.HandleFailed(w, req)
}

func (s *Server) do(Net string, w dns.ResponseWriter, req *dns.Msg) {
	if len(req.Question) != 1 {
		dns.HandleFailed(w, req)
		return
	}
	zone, name := s.zones.match(req.Question[0].Name, req.Question[0].Qtype)
	if zone == nil {
		s.recurse(w, req)
		return
	}

	s.zones.Lock()
	defer s.zones.Unlock()

	m := new(dns.Msg)
	m.SetReply(req)

	var answerKnown bool
	dnsreq := dns.RR_Header{Name: req.Question[0].Name, Rrtype: req.Question[0].Qtype, Class: req.Question[0].Qclass}
	for _, r := range (*zone)[dnsreq] {
		m.Answer = append(m.Answer, r)
		answerKnown = true
	}
	// TODO: more logs here
	s.roundRobin(zone, dnsreq)

	if !answerKnown && s.recurseTo != "" {
		s.recurse(w, req)
		return
	}

	// Add Authority section
	for _, r := range (*zone)[dns.RR_Header{Name: name, Rrtype: dns.TypeNS, Class: dns.ClassINET}] {
		m.Ns = append(m.Ns, r)
		// Resolve Authority if possible and serve as Extra
		for _, r := range (*zone)[dns.RR_Header{Name: r.(*dns.NS).Ns, Rrtype: dns.TypeA, Class: dns.ClassINET}] {
			m.Extra = append(m.Extra, r)
		}
		for _, r := range (*zone)[dns.RR_Header{Name: r.(*dns.NS).Ns, Rrtype: dns.TypeAAAA, Class: dns.ClassINET}] {
			m.Extra = append(m.Extra, r)
		}
	}

	m.Authoritative = true
	w.WriteMsg(m)
}

func (s *Server) DoTCP(w dns.ResponseWriter, req *dns.Msg) {
	s.do("tcp", w, req)
}

func (s *Server) DoUDP(w dns.ResponseWriter, req *dns.Msg) {
	s.do("udp", w, req)
}
