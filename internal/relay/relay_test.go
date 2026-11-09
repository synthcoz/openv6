package relay

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"openv6/internal/proto"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestRelayLoopback(t *testing.T) {
	// fake proxy on the client node: echoes a greeting
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	tport := target.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := target.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, _ := bufio.NewReader(c).ReadString('\n')
				fmt.Fprintf(c, "echo %s", line)
			}(c)
		}
	}()

	ctl, pub := freePort(t), freePort(t)
	srv := NewServer(t.TempDir())
	defer srv.Close()
	if errs := srv.Apply(&proto.RelayServerSpec{Port: ctl, Clients: []proto.RelayAuth{{NodeID: 7, Token: "tok", Maps: []proto.RelayMap{{Public: pub, Target: tport}}}}}); len(errs) > 0 {
		t.Fatal(errs)
	}
	fp := srv.Fingerprint()
	if len(fp) != 64 {
		t.Fatalf("fingerprint %q", fp)
	}

	// wrong fingerprint must not connect
	bad := NewClient()
	bad.Apply([]proto.RelayClientSpec{{ServerNodeID: 1, Addr: fmt.Sprintf("127.0.0.1:%d", ctl), Token: "tok", Fingerprint: strings.Repeat("0", 64), Ports: []int{tport}}})
	time.Sleep(500 * time.Millisecond)
	if st := bad.Status(); len(st) != 1 || st[0].Connected {
		t.Fatalf("bad fingerprint connected: %+v", st)
	}
	bad.Close()

	cl := NewClient()
	defer cl.Close()
	cl.Apply([]proto.RelayClientSpec{{ServerNodeID: 1, Addr: fmt.Sprintf("127.0.0.1:%d", ctl), Token: "tok", Fingerprint: fp, Ports: []int{tport}}})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if st := cl.Status(); len(st) == 1 && st[0].Connected && len(srv.Connected()) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not connected: %+v", cl.Status())
		}
		time.Sleep(50 * time.Millisecond)
	}

	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", pub), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprint(c, "hello\n")
	b, _ := io.ReadAll(c)
	c.Close()
	if string(b) != "echo hello\n" {
		t.Fatalf("got %q", b)
	}

	// port no longer allowed by the client: connection is dropped
	cl.Apply([]proto.RelayClientSpec{{ServerNodeID: 1, Addr: fmt.Sprintf("127.0.0.1:%d", ctl), Token: "tok", Fingerprint: fp, Ports: nil}})
	c, err = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", pub), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprint(c, "hello\n")
	b, _ = io.ReadAll(c)
	c.Close()
	if len(b) != 0 {
		t.Fatalf("disallowed port answered %q", b)
	}

	// token revoked on the server: session dropped
	srv.Apply(&proto.RelayServerSpec{Port: ctl})
	time.Sleep(300 * time.Millisecond)
	if n := len(srv.Connected()); n != 0 {
		t.Fatalf("still connected: %d", n)
	}
}
