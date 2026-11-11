package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"syscall"
)

const (
	socksVer     = 0x05
	methodNone   = 0x00
	methodUser   = 0x02
	methodNoAcc  = 0xff
	cmdConnect   = 0x01
	atypIPv4     = 0x01
	atypDomain   = 0x03
	atypIPv6     = 0x04
	repOK        = 0x00
	repFail      = 0x01
	repNotAllow  = 0x02
	repNetUnr    = 0x03
	repHostUnr   = 0x04
	repRefused   = 0x05
	repCmdNotSup = 0x07
)

func (l *listener) serveSOCKS5(c *bufConn, cfg *Config) {
	// greeting
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil || hdr[0] != socksVer {
		return
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}
	want := byte(methodNone)
	if cfg.authRequired() {
		want = methodUser
	}
	found := false
	for _, m := range methods {
		if m == want {
			found = true
		}
	}
	if !found {
		_, _ = c.Write([]byte{socksVer, methodNoAcc})
		return
	}
	_, _ = c.Write([]byte{socksVer, want})
	if want == methodUser {
		// RFC 1929
		h := make([]byte, 2)
		if _, err := io.ReadFull(c, h); err != nil || h[0] != 0x01 {
			return
		}
		u := make([]byte, h[1])
		if _, err := io.ReadFull(c, u); err != nil {
			return
		}
		pl := make([]byte, 1)
		if _, err := io.ReadFull(c, pl); err != nil {
			return
		}
		p := make([]byte, pl[0])
		if _, err := io.ReadFull(c, p); err != nil {
			return
		}
		if !cfg.checkAuth(string(u), string(p)) {
			_, _ = c.Write([]byte{0x01, 0x01})
			return
		}
		_, _ = c.Write([]byte{0x01, 0x00})
	}
	// request
	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil || req[0] != socksVer {
		return
	}
	host, port, err := readSocksAddr(c, req[3])
	if err != nil {
		return
	}
	if req[1] != cmdConnect {
		// UDP ASSOCIATE / BIND: not supported yet (see docs/ROADMAP.md)
		writeSocksReply(c, repCmdNotSup)
		return
	}
	target, err := l.dial(context.Background(), cfg, host, port)
	if err != nil {
		writeSocksReply(c, socksErrCode(err))
		return
	}
	writeSocksReply(c, repOK)
	l.pipe(c, target)
}

func readSocksAddr(r io.Reader, atyp byte) (string, int, error) {
	var host string
	switch atyp {
	case atypIPv4:
		b := make([]byte, 4)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, err
		}
		host = net.IP(b).String()
	case atypIPv6:
		b := make([]byte, 16)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, err
		}
		host = net.IP(b).String()
	case atypDomain:
		lb := make([]byte, 1)
		if _, err := io.ReadFull(r, lb); err != nil {
			return "", 0, err
		}
		b := make([]byte, lb[0])
		if _, err := io.ReadFull(r, b); err != nil {
			return "", 0, err
		}
		host = string(b)
	default:
		return "", 0, errors.New("bad atyp")
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(r, pb); err != nil {
		return "", 0, err
	}
	return host, int(binary.BigEndian.Uint16(pb)), nil
}

func writeSocksReply(w io.Writer, rep byte) {
	// bound address is not meaningful to clients; send 0.0.0.0:0
	_, _ = w.Write([]byte{socksVer, rep, 0x00, atypIPv4, 0, 0, 0, 0, 0, 0})
}

func socksErrCode(err error) byte {
	switch {
	case errors.Is(err, ErrBlockedByRule), errors.Is(err, ErrBlocked):
		return repNotAllow
	case errors.Is(err, ErrNoIPv6):
		return repHostUnr
	case errors.Is(err, syscall.ECONNREFUSED):
		return repRefused
	case errors.Is(err, syscall.ENETUNREACH):
		return repNetUnr
	case errors.Is(err, syscall.EHOSTUNREACH):
		return repHostUnr
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return repHostUnr
	}
	return repFail
}
