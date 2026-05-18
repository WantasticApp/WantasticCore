package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"WantasticCore/internal/wg/userspace"

	"github.com/rs/zerolog/log"
	"golang.org/x/net/dns/dnsmessage"
)

const (
	tenantDNSPort       = 53
	tenantDNSDomainBase = "wantastic.internal"
	tenantDNSTTL        = 30 * time.Second
	tenantDNSCacheTTL   = time.Minute
	tenantDNSUpstream   = "1.1.1.1:53"
)

type tenantDNSServer struct {
	accountID string
	device    *userspace.TenantDevice
	peerIndex func(accountID string) []tenantDNSPeerRecord
	resolver  tenantDNSResolver

	ctx    context.Context
	cancel context.CancelFunc

	udpConn io.Closer
	tcpLn   io.Closer

	wg sync.WaitGroup

	mu           sync.Mutex
	cached       *tenantDNSSnapshot
	cacheExpires time.Time
}

type tenantDNSSnapshot struct {
	domain  string
	forward map[string][]netip.Addr
	reverse map[string]string
	known   map[string]struct{}
}

type tenantDNSResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

type tenantDNSPeerRecord struct {
	PeerID     string
	Name       string
	AssignedIP string
}

func newTenantDNSServer(ctx context.Context, accountID string, device *userspace.TenantDevice, peerIndex func(accountID string) []tenantDNSPeerRecord) (*tenantDNSServer, error) {
	if device == nil {
		return nil, fmt.Errorf("tenant device is nil")
	}
	if peerIndex == nil {
		return nil, fmt.Errorf("tenant dns peer index is nil")
	}

	listenAddr := netip.AddrPortFrom(device.DeviceIP, tenantDNSPort)

	udpConn, err := device.Net.ListenUDPAddrPort(listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen udp %s: %w", listenAddr, err)
	}

	tcpLn, err := device.Net.ListenTCPAddrPort(listenAddr)
	if err != nil {
		_ = udpConn.Close()
		return nil, fmt.Errorf("listen tcp %s: %w", listenAddr, err)
	}

	srvCtx, cancel := context.WithCancel(ctx)
	srv := &tenantDNSServer{
		accountID: accountID,
		device:    device,
		peerIndex: peerIndex,
		resolver:  newTenantDNSUpstreamResolver(),
		ctx:       srvCtx,
		cancel:    cancel,
		udpConn:   udpConn,
		tcpLn:     tcpLn,
	}

	srv.wg.Add(2)
	go srv.serveUDP(udpConn)
	go srv.serveTCP(tcpLn)

	log.Debug().
		Str("account_id", accountID).
		Str("listen_addr", listenAddr.String()).
		Msg("Started tenant overlay DNS server")

	return srv, nil
}

func (s *tenantDNSServer) stop() {
	s.cancel()
	if s.udpConn != nil {
		_ = s.udpConn.Close()
	}
	if s.tcpLn != nil {
		_ = s.tcpLn.Close()
	}
	s.wg.Wait()
}

func (s *tenantDNSServer) invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cached = nil
	s.cacheExpires = time.Time{}
}

func (s *tenantDNSServer) serveUDP(conn interface {
	ReadFrom([]byte) (int, net.Addr, error)
	WriteTo([]byte, net.Addr) (int, error)
	Close() error
}) {
	defer s.wg.Done()

	buf := make([]byte, 2048)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if s.ctx.Err() == nil {
				log.Debug().Err(err).Str("account_id", s.accountID).Msg("Tenant DNS UDP listener stopped")
			}
			return
		}

		resp, err := s.handleQuery(buf[:n])
		if err != nil || len(resp) == 0 {
			continue
		}
		if _, err := conn.WriteTo(resp, addr); err != nil && s.ctx.Err() == nil {
			log.Debug().Err(err).Str("account_id", s.accountID).Msg("Tenant DNS UDP write failed")
		}
	}
}

func (s *tenantDNSServer) serveTCP(listener interface {
	Accept() (net.Conn, error)
	Close() error
}) {
	defer s.wg.Done()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if s.ctx.Err() == nil {
				log.Debug().Err(err).Str("account_id", s.accountID).Msg("Tenant DNS TCP listener stopped")
			}
			return
		}

		s.wg.Add(1)
		go func(c io.ReadWriteCloser) {
			defer s.wg.Done()
			defer c.Close()

			var lenBuf [2]byte
			if _, err := io.ReadFull(c, lenBuf[:]); err != nil {
				return
			}
			msgLen := int(binary.BigEndian.Uint16(lenBuf[:]))
			if msgLen <= 0 || msgLen > 4096 {
				return
			}

			msg := make([]byte, msgLen)
			if _, err := io.ReadFull(c, msg); err != nil {
				return
			}

			resp, err := s.handleQuery(msg)
			if err != nil || len(resp) == 0 {
				return
			}

			binary.BigEndian.PutUint16(lenBuf[:], uint16(len(resp)))
			if _, err := c.Write(lenBuf[:]); err != nil {
				return
			}
			_, _ = c.Write(resp)
		}(conn)
	}
}

func (s *tenantDNSServer) handleQuery(msg []byte) ([]byte, error) {
	var parser dnsmessage.Parser
	reqHeader, err := parser.Start(msg)
	if err != nil {
		return nil, err
	}

	questions := make([]dnsmessage.Question, 0, 1)
	for {
		q, err := parser.Question()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			return nil, err
		}
		questions = append(questions, q)
	}
	if len(questions) == 0 {
		return nil, nil
	}

	snapshot, err := s.snapshot()
	if err != nil {
		return nil, err
	}

	rcode, authoritative, recursionAvailable, answers := s.resolveQuestion(snapshot, questions[0])
	respHeader := dnsmessage.Header{
		ID:                 reqHeader.ID,
		Response:           true,
		Authoritative:      authoritative,
		RecursionDesired:   reqHeader.RecursionDesired,
		RecursionAvailable: recursionAvailable,
		RCode:              rcode,
	}

	builder := dnsmessage.NewBuilder(nil, respHeader)
	builder.EnableCompression()
	if err := builder.StartQuestions(); err != nil {
		return nil, err
	}
	for _, q := range questions {
		if err := builder.Question(q); err != nil {
			return nil, err
		}
	}
	if err := builder.StartAnswers(); err != nil {
		return nil, err
	}
	for _, answer := range answers {
		if err := answer.append(&builder); err != nil {
			return nil, err
		}
	}
	return builder.Finish()
}

func (s *tenantDNSServer) snapshot() (*tenantDNSSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if s.cached != nil && now.Before(s.cacheExpires) {
		return s.cached, nil
	}

	peers := s.peerIndex(s.accountID)
	s.cached = buildTenantDNSSnapshot(s.accountID, s.device.DeviceIP, peers)
	s.cacheExpires = now.Add(tenantDNSCacheTTL)
	return s.cached, nil
}

func newTenantDNSUpstreamResolver() tenantDNSResolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			dialer := &net.Dialer{}
			if strings.TrimSpace(network) == "" {
				network = "udp"
			}
			return dialer.DialContext(ctx, network, tenantDNSUpstream)
		},
	}
}

func (s *tenantDNSServer) resolveQuestion(snapshot *tenantDNSSnapshot, question dnsmessage.Question) (dnsmessage.RCode, bool, bool, []dnsAnswer) {
	recursionAvailable := s != nil && s.resolver != nil
	rcode, answers := snapshot.answer(question)
	if rcode != dnsmessage.RCodeNameError {
		return rcode, true, recursionAvailable, answers
	}

	upstreamRcode, upstreamAnswers, ok := s.resolveUpstreamQuestion(snapshot, question)
	if ok {
		return upstreamRcode, false, recursionAvailable, upstreamAnswers
	}
	return rcode, true, recursionAvailable, answers
}

func (s *tenantDNSServer) resolveUpstreamQuestion(snapshot *tenantDNSSnapshot, question dnsmessage.Question) (dnsmessage.RCode, []dnsAnswer, bool) {
	if s == nil || s.resolver == nil || snapshot == nil {
		return dnsmessage.RCodeServerFailure, nil, false
	}

	name := normalizeDNSName(question.Name.String())
	if !snapshot.shouldResolveUpstream(name) {
		return dnsmessage.RCodeNameError, nil, false
	}

	ctx := context.Background()
	if s.ctx != nil {
		ctx = s.ctx
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	switch question.Type {
	case dnsmessage.TypeA:
		rcode, answers := s.lookupUpstreamIPs(ctx, question.Name.String(), name, "ip4")
		return rcode, answers, true
	case dnsmessage.TypeAAAA:
		rcode, answers := s.lookupUpstreamIPs(ctx, question.Name.String(), name, "ip6")
		return rcode, answers, true
	case dnsmessage.TypeALL:
		rcode, answers := s.lookupUpstreamIPs(ctx, question.Name.String(), name, "ip")
		return rcode, answers, true
	case dnsmessage.TypePTR:
		targetIP, ok := ptrNameToAddr(name)
		if !ok {
			return dnsmessage.RCodeNameError, nil, false
		}
		rcode, answers := s.lookupUpstreamPTR(ctx, question.Name.String(), targetIP)
		return rcode, answers, true
	default:
		return dnsmessage.RCodeNameError, nil, false
	}
}

func (s *tenantDNSServer) lookupUpstreamIPs(ctx context.Context, responseName, host, network string) (dnsmessage.RCode, []dnsAnswer) {
	addrs, err := s.resolver.LookupNetIP(ctx, network, host)
	if err != nil {
		return mapLookupErrorToRCode(err), nil
	}

	answers := make([]dnsAnswer, 0, len(addrs))
	for _, addr := range addrs {
		if addr.Is4() {
			answers = append(answers, dnsAnswer{name: responseName, typ: dnsmessage.TypeA, a: addr})
		} else if addr.Is6() {
			answers = append(answers, dnsAnswer{name: responseName, typ: dnsmessage.TypeAAAA, a: addr})
		}
	}
	return dnsmessage.RCodeSuccess, answers
}

func (s *tenantDNSServer) lookupUpstreamPTR(ctx context.Context, responseName string, addr netip.Addr) (dnsmessage.RCode, []dnsAnswer) {
	names, err := s.resolver.LookupAddr(ctx, addr.String())
	if err != nil {
		return mapLookupErrorToRCode(err), nil
	}
	answers := make([]dnsAnswer, 0, len(names))
	for _, name := range names {
		name = normalizeDNSName(name)
		if name == "" {
			continue
		}
		answers = append(answers, dnsAnswer{name: responseName, typ: dnsmessage.TypePTR, ptr: name})
	}
	return dnsmessage.RCodeSuccess, answers
}

func mapLookupErrorToRCode(err error) dnsmessage.RCode {
	if err == nil {
		return dnsmessage.RCodeSuccess
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return dnsmessage.RCodeNameError
		}
	}
	return dnsmessage.RCodeServerFailure
}

type dnsAnswer struct {
	name string
	typ  dnsmessage.Type
	a    netip.Addr
	ptr  string
}

func (a dnsAnswer) append(builder *dnsmessage.Builder) error {
	name := mustDNSName(a.name)
	header := dnsmessage.ResourceHeader{
		Name:  name,
		Type:  a.typ,
		Class: dnsmessage.ClassINET,
		TTL:   uint32(tenantDNSTTL / time.Second),
	}
	switch a.typ {
	case dnsmessage.TypeA:
		ip4 := a.a.As4()
		return builder.AResource(header, dnsmessage.AResource{A: ip4})
	case dnsmessage.TypeAAAA:
		ip6 := a.a.As16()
		return builder.AAAAResource(header, dnsmessage.AAAAResource{AAAA: ip6})
	case dnsmessage.TypePTR:
		return builder.PTRResource(header, dnsmessage.PTRResource{PTR: mustDNSName(a.ptr)})
	default:
		return nil
	}
}

func buildTenantDNSSnapshot(accountID string, serverIP netip.Addr, peers []tenantDNSPeerRecord) *tenantDNSSnapshot {
	domain := tenantDNSDomain(accountID)
	snapshot := &tenantDNSSnapshot{
		domain:  domain,
		forward: make(map[string][]netip.Addr),
		reverse: make(map[string]string),
		known:   make(map[string]struct{}),
	}

	addHost := func(name string, addr netip.Addr) {
		normalized := normalizeDNSName(name)
		if normalized == "" || !addr.IsValid() {
			return
		}
		for _, existing := range snapshot.forward[normalized] {
			if existing == addr {
				snapshot.known[normalized] = struct{}{}
				return
			}
		}
		snapshot.forward[normalized] = append(snapshot.forward[normalized], addr)
		snapshot.known[normalized] = struct{}{}
	}

	for _, name := range []string{
		"router",
		"gateway",
		"gw",
		domain,
		"router." + domain,
		"gateway." + domain,
		"gw." + domain,
	} {
		addHost(name, serverIP)
	}
	if serverIP.Is4() {
		snapshot.reverse[reversePTRName(serverIP)] = "router." + domain
	}

	for _, peer := range peers {
		addr, ok := parseAssignedIP(peer.AssignedIP)
		if !ok {
			continue
		}

		labels := make([]string, 0, 3)
		if label := sanitizeDNSLabel(peer.Name); label != "" {
			labels = append(labels, label)
		}
		synth := syntheticPeerHost(addr)
		labels = append(labels, synth)

		for _, label := range labels {
			addHost(label, addr)
			addHost(label+"."+domain, addr)
		}

		if addr.Is4() {
			ptrTarget := synth + "." + domain
			if len(labels) > 0 && labels[0] != synth {
				ptrTarget = labels[0] + "." + domain
			}
			snapshot.reverse[reversePTRName(addr)] = ptrTarget
		}
	}

	return snapshot
}

func (s *tenantDNSSnapshot) answer(question dnsmessage.Question) (dnsmessage.RCode, []dnsAnswer) {
	name := normalizeDNSName(question.Name.String())
	switch question.Type {
	case dnsmessage.TypeA:
		return s.answerAddress(name, false)
	case dnsmessage.TypeAAAA:
		return s.answerAddress(name, true)
	case dnsmessage.TypePTR:
		ptr, ok := s.reverse[name]
		if !ok {
			return dnsmessage.RCodeNameError, nil
		}
		return dnsmessage.RCodeSuccess, []dnsAnswer{{name: question.Name.String(), typ: dnsmessage.TypePTR, ptr: ptr}}
	case dnsmessage.TypeALL:
		if answers, ok := s.answerAll(name); ok {
			return dnsmessage.RCodeSuccess, answers
		}
		return dnsmessage.RCodeNameError, nil
	default:
		if _, ok := s.known[name]; ok {
			return dnsmessage.RCodeSuccess, nil
		}
		return dnsmessage.RCodeNameError, nil
	}
}

func (s *tenantDNSSnapshot) answerAll(name string) ([]dnsAnswer, bool) {
	ips, ok := s.forward[name]
	if !ok {
		return nil, false
	}
	answers := make([]dnsAnswer, 0, len(ips))
	for _, ip := range ips {
		if ip.Is4() {
			answers = append(answers, dnsAnswer{name: name, typ: dnsmessage.TypeA, a: ip})
		}
		if ip.Is6() {
			answers = append(answers, dnsAnswer{name: name, typ: dnsmessage.TypeAAAA, a: ip})
		}
	}
	return answers, true
}

func (s *tenantDNSSnapshot) answerAddress(name string, wantV6 bool) (dnsmessage.RCode, []dnsAnswer) {
	ips, ok := s.forward[name]
	if !ok {
		return dnsmessage.RCodeNameError, nil
	}
	answers := make([]dnsAnswer, 0, len(ips))
	for _, ip := range ips {
		if wantV6 && ip.Is6() {
			answers = append(answers, dnsAnswer{name: name, typ: dnsmessage.TypeAAAA, a: ip})
		}
		if !wantV6 && ip.Is4() {
			answers = append(answers, dnsAnswer{name: name, typ: dnsmessage.TypeA, a: ip})
		}
	}
	if len(answers) == 0 {
		return dnsmessage.RCodeSuccess, nil
	}
	return dnsmessage.RCodeSuccess, answers
}

func (s *tenantDNSSnapshot) shouldResolveUpstream(name string) bool {
	if s == nil || name == "" {
		return false
	}
	if _, ok := s.known[name]; ok {
		return false
	}
	if _, ok := s.reverse[name]; ok {
		return false
	}
	if name == s.domain || strings.HasSuffix(name, "."+s.domain) {
		return false
	}
	if name == tenantDNSDomainBase || strings.HasSuffix(name, "."+tenantDNSDomainBase) {
		return false
	}
	return true
}

func tenantDNSDomain(accountID string) string {
	label := sanitizeDNSLabel(accountID)
	if label == "" {
		label = "tenant"
	}
	return label + "." + tenantDNSDomainBase
}

func sanitizeDNSLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}

	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}

	label := strings.Trim(b.String(), "-")
	if len(label) > 63 {
		label = strings.Trim(label[:63], "-")
	}
	return label
}

func syntheticPeerHost(addr netip.Addr) string {
	if !addr.IsValid() {
		return "peer"
	}
	return "peer-" + strings.NewReplacer(".", "-", ":", "-").Replace(addr.String())
}

func parseAssignedIP(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Addr{}, false
	}
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return netip.Addr{}, false
		}
		return prefix.Addr(), true
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr, true
}

func normalizeDNSName(name string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
}

func ensureTrailingDot(name string) string {
	if name == "" {
		return "."
	}
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}

func mustDNSName(name string) dnsmessage.Name {
	dnsName, err := dnsmessage.NewName(ensureTrailingDot(normalizeDNSName(name)))
	if err != nil {
		panic(err)
	}
	return dnsName
}

func reversePTRName(addr netip.Addr) string {
	if addr.Is4() {
		ip4 := addr.As4()
		return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa", ip4[3], ip4[2], ip4[1], ip4[0])
	}
	return ""
}

func ptrNameToAddr(name string) (netip.Addr, bool) {
	const suffix = ".in-addr.arpa"
	if !strings.HasSuffix(name, suffix) {
		return netip.Addr{}, false
	}

	trimmed := strings.TrimSuffix(name, suffix)
	parts := strings.Split(trimmed, ".")
	if len(parts) != 4 {
		return netip.Addr{}, false
	}

	var octets [4]byte
	for i := range parts {
		n, err := strconv.Atoi(parts[3-i])
		if err != nil || n < 0 || n > 255 {
			return netip.Addr{}, false
		}
		octets[i] = byte(n)
	}
	return netip.AddrFrom4(octets), true
}
