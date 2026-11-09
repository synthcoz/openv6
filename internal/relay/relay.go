// Package relay lets a node behind NAT/CGNAT (e.g. a home machine) publish
// proxies through a node with a public IP (e.g. a VPS).
//
//	client app ──► VPS:publicPort ──(TLS + yamux stream)──► home agent ──► 127.0.0.1:proxyPort
//
// The home agent dials the VPS (outgoing only, so CGNAT is fine), proves its
// identity with a token and pins the VPS certificate fingerprint. The VPS
// opens one multiplexed stream per incoming connection; the stream starts
// with the 2-byte target port, which the home agent checks against the ports
// the panel allowed.
package relay

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/yamux"

	"openv6/internal/proto"
)

func yamuxConfig() *yamux.Config {
	c := yamux.DefaultConfig()
	c.LogOutput = io.Discard
	c.KeepAliveInterval = 20 * time.Second
	c.ConnectionWriteTimeout = 30 * time.Second
	return c
}

func splice(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
	_ = a.Close()
	_ = b.Close()
}

// ---------- server (public node) ----------

type Server struct {
	mu       sync.Mutex
	dataDir  string
	port     int
	ctl      net.Listener
	cert     tls.Certificate
	fp       string
	auth     map[string]int64 // token -> client node id
	sessions map[int64]*yamux.Session
	pubs     map[int]*pubListener // public port -> listener
}

type pubListener struct {
	ln     net.Listener
	client int64
	target int
}

func NewServer(dataDir string) *Server {
	return &Server{dataDir: dataDir, auth: map[string]int64{}, sessions: map[int64]*yamux.Session{}, pubs: map[int]*pubListener{}}
}

// Fingerprint of the server certificate ("" when the relay is off).
func (s *Server) Fingerprint() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fp
}

// Apply starts/stops the control listener and public listeners.
func (s *Server) Apply(spec *proto.RelayServerSpec) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []string
	if spec == nil || spec.Port == 0 {
		s.stopLocked()
		return nil
	}
	if s.ctl == nil || s.port != spec.Port {
		s.stopLocked()
		if err := s.loadCertLocked(); err != nil {
			return []string{"relay certificate: " + err.Error()}
		}
		ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", spec.Port), &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12})
		if err != nil {
			return []string{fmt.Sprintf("relay port %d: %v", spec.Port, err)}
		}
		s.ctl, s.port = ln, spec.Port
		go s.acceptControl(ln)
		log.Printf("relay: listening for nodes on :%d", spec.Port)
	}
	s.auth = map[string]int64{}
	want := map[int]pubListener{}
	for _, c := range spec.Clients {
		s.auth[c.Token] = c.NodeID
		for _, m := range c.Maps {
			want[m.Public] = pubListener{client: c.NodeID, target: m.Target}
		}
	}
	for port, p := range s.pubs {
		if w, ok := want[port]; !ok || w.client != p.client || w.target != p.target {
			_ = p.ln.Close()
			delete(s.pubs, port)
		}
	}
	for port, w := range want {
		if _, ok := s.pubs[port]; ok {
			continue
		}
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			errs = append(errs, fmt.Sprintf("relay public port %d: %v", port, err))
			continue
		}
		p := &pubListener{ln: ln, client: w.client, target: w.target}
		s.pubs[port] = p
		go s.acceptPublic(p)
	}
	// drop sessions of clients that are no longer allowed
	allowed := map[int64]bool{}
	for _, id := range s.auth {
		allowed[id] = true
	}
	for id, sess := range s.sessions {
		if !allowed[id] {
			_ = sess.Close()
			delete(s.sessions, id)
		}
	}
	return errs
}

func (s *Server) stopLocked() {
	if s.ctl != nil {
		_ = s.ctl.Close()
		s.ctl = nil
	}
	for port, p := range s.pubs {
		_ = p.ln.Close()
		delete(s.pubs, port)
	}
	for id, sess := range s.sessions {
		_ = sess.Close()
		delete(s.sessions, id)
	}
	s.fp = ""
}

func (s *Server) Close() { s.mu.Lock(); s.stopLocked(); s.mu.Unlock() }

// Connected lists client node ids with a live session.
func (s *Server) Connected() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []int64
	for id, sess := range s.sessions {
		if !sess.IsClosed() {
			out = append(out, id)
		}
	}
	return out
}

func (s *Server) acceptControl(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go s.handleControl(c)
	}
}

func (s *Server) handleControl(c net.Conn) {
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	br := bufio.NewReader(io.LimitReader(c, 256))
	line, err := br.ReadString('\n')
	if err != nil {
		_ = c.Close()
		return
	}
	tok := strings.TrimSpace(strings.TrimPrefix(line, "OPENV6-RELAY "))
	s.mu.Lock()
	var id int64
	found := false
	for t, n := range s.auth {
		if subtle.ConstantTimeCompare([]byte(t), []byte(tok)) == 1 {
			id, found = n, true
		}
	}
	s.mu.Unlock()
	if !found {
		_, _ = io.WriteString(c, "DENIED\n")
		_ = c.Close()
		return
	}
	if _, err := io.WriteString(c, "OK\n"); err != nil {
		_ = c.Close()
		return
	}
	_ = c.SetDeadline(time.Time{})
	sess, err := yamux.Client(c, yamuxConfig())
	if err != nil {
		_ = c.Close()
		return
	}
	s.mu.Lock()
	if old := s.sessions[id]; old != nil {
		_ = old.Close()
	}
	s.sessions[id] = sess
	s.mu.Unlock()
	log.Printf("relay: node %d connected from %s", id, c.RemoteAddr())
}

func (s *Server) acceptPublic(p *pubListener) {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			s.mu.Lock()
			sess := s.sessions[p.client]
			s.mu.Unlock()
			if sess == nil || sess.IsClosed() {
				_ = c.Close()
				return
			}
			st, err := sess.Open()
			if err != nil {
				_ = c.Close()
				return
			}
			var hdr [2]byte
			binary.BigEndian.PutUint16(hdr[:], uint16(p.target))
			if _, err := st.Write(hdr[:]); err != nil {
				_ = c.Close()
				_ = st.Close()
				return
			}
			splice(c, st)
		}()
	}
}

// loadCertLocked loads or creates the self-signed relay certificate.
func (s *Server) loadCertLocked() error {
	crt, key := filepath.Join(s.dataDir, "relay.crt"), filepath.Join(s.dataDir, "relay.key")
	if _, err := os.Stat(crt); err != nil {
		if err := generateCert(crt, key); err != nil {
			return err
		}
	}
	cert, err := tls.LoadX509KeyPair(crt, key)
	if err != nil {
		return err
	}
	s.cert = cert
	sum := sha256.Sum256(cert.Certificate[0])
	s.fp = hex.EncodeToString(sum[:])
	return nil
}

func generateCert(crtPath, keyPath string) error {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "openv6 relay"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(20, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		return err
	}
	kb, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(crtPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return err
	}
	return os.WriteFile(crtPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

// ---------- client (node behind NAT) ----------

type Client struct {
	mu    sync.Mutex
	conns map[int64]*clientConn // server node id -> connection
}

type clientConn struct {
	spec   proto.RelayClientSpec
	stop   chan struct{}
	mu     sync.Mutex
	ports  map[int]bool
	state  string
	errMsg string
}

func NewClient() *Client { return &Client{conns: map[int64]*clientConn{}} }

func (c *Client) Apply(specs []proto.RelayClientSpec) {
	c.mu.Lock()
	defer c.mu.Unlock()
	want := map[int64]proto.RelayClientSpec{}
	for _, s := range specs {
		want[s.ServerNodeID] = s
	}
	for id, cc := range c.conns {
		w, ok := want[id]
		if !ok || w.Addr != cc.spec.Addr || w.Token != cc.spec.Token || w.Fingerprint != cc.spec.Fingerprint {
			close(cc.stop)
			delete(c.conns, id)
		}
	}
	for id, s := range want {
		cc := c.conns[id]
		if cc == nil {
			cc = &clientConn{spec: s, stop: make(chan struct{}), state: "connecting"}
			c.conns[id] = cc
			go cc.run()
		}
		ports := map[int]bool{}
		for _, p := range s.Ports {
			ports[p] = true
		}
		cc.mu.Lock()
		cc.ports = ports
		cc.mu.Unlock()
	}
}

func (c *Client) Close() { c.Apply(nil) }

// Status reports each relay connection.
func (c *Client) Status() []proto.RelayState {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []proto.RelayState
	for id, cc := range c.conns {
		cc.mu.Lock()
		out = append(out, proto.RelayState{Role: "client", PeerNodeID: id, Peer: cc.spec.Addr, Connected: cc.state == "connected", Error: cc.errMsg})
		cc.mu.Unlock()
	}
	return out
}

func (cc *clientConn) set(state, err string) {
	cc.mu.Lock()
	cc.state, cc.errMsg = state, err
	cc.mu.Unlock()
}

func (cc *clientConn) run() {
	backoff := time.Second
	for {
		connected, err := cc.session()
		if connected {
			backoff = time.Second
		}
		select {
		case <-cc.stop:
			return
		default:
		}
		if err != nil {
			cc.set("error", err.Error())
		}
		select {
		case <-cc.stop:
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

var errFingerprint = errors.New("relay certificate does not match (possible interception)")

func (cc *clientConn) session() (bool, error) {
	fp := strings.ToLower(cc.spec.Fingerprint)
	cfg := &tls.Config{
		InsecureSkipVerify: true, // self-signed: verified by fingerprint below
		MinVersion:         tls.VersionTLS12,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errFingerprint
			}
			sum := sha256.Sum256(raw[0])
			if fp == "" || hex.EncodeToString(sum[:]) != fp {
				return errFingerprint
			}
			return nil
		},
	}
	d := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	conn, err := tls.DialWithDialer(d, "tcp", cc.spec.Addr, cfg)
	if err != nil {
		return false, err
	}
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := io.WriteString(conn, "OPENV6-RELAY "+cc.spec.Token+"\n"); err != nil {
		conn.Close()
		return false, err
	}
	line, err := bufio.NewReader(io.LimitReader(conn, 64)).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "OK" {
		conn.Close()
		return false, fmt.Errorf("relay refused the node (token)")
	}
	_ = conn.SetDeadline(time.Time{})
	sess, err := yamux.Server(conn, yamuxConfig())
	if err != nil {
		conn.Close()
		return false, err
	}
	defer sess.Close()
	cc.set("connected", "")
	log.Printf("relay: connected to %s", cc.spec.Addr)
	go func() {
		select {
		case <-cc.stop:
			_ = sess.Close()
		case <-sess.CloseChan():
		}
	}()
	for {
		st, err := sess.Accept()
		if err != nil {
			return true, fmt.Errorf("relay connection lost: %v", err)
		}
		go cc.serve(st)
	}
}

func (cc *clientConn) serve(st net.Conn) {
	var hdr [2]byte
	_ = st.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(st, hdr[:]); err != nil {
		_ = st.Close()
		return
	}
	_ = st.SetReadDeadline(time.Time{})
	port := int(binary.BigEndian.Uint16(hdr[:]))
	cc.mu.Lock()
	ok := cc.ports[port]
	cc.mu.Unlock()
	if !ok {
		_ = st.Close()
		return
	}
	t, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		_ = st.Close()
		return
	}
	splice(st, t)
}
