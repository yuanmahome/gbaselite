package server

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log"
	"net"
	"syscall"
	"testing"
	"time"
)

func requireIPv6Loopback(t *testing.T) {
	t.Helper()
	l, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	l.Close()
}

func TestLoopbackMySQLBothFamilies(t *testing.T) {
	requireIPv6Loopback(t)
	for _, host := range []string{"127.0.0.1", "localhost"} {
		t.Run(host, func(t *testing.T) {
			e, err := openTestEngine(t, t.TempDir(), "root", "secret")
			if err != nil {
				t.Fatal(err)
			}
			l, err := Listen(net.JoinHostPort(host, "0"))
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			dual, ok := l.(*loopbackListener)
			if !ok {
				t.Fatal("missing IPv6 loopback listener")
			}
			_, port, _ := net.SplitHostPort(l.Addr().String())
			for _, bound := range dual.listeners {
				addr := bound.Addr().(*net.TCPAddr)
				if !addr.IP.IsLoopback() {
					t.Fatalf("non-loopback listener: %s", addr)
				}
				_, otherPort, _ := net.SplitHostPort(addr.String())
				if otherPort != port {
					t.Fatalf("ports differ: %s vs %s", otherPort, port)
				}
			}
			srv := &MySQLServer{Engine: e, Logger: log.New(io.Discard, "", 0), MaxConnections: 2}
			done := make(chan error, 1)
			go func() { done <- srv.Serve(l) }()
			for _, clientHost := range []string{"127.0.0.1", "::1", "localhost"} {
				client, err := sql.Open("mysql", "root:secret@tcp("+net.JoinHostPort(clientHost, port)+")/?timeout=1s&readTimeout=1s")
				if err != nil {
					t.Fatal(err)
				}
				var value int
				err = client.QueryRow("SELECT 1").Scan(&value)
				client.Close()
				if err != nil || value != 1 {
					t.Fatalf("host %s: value=%d err=%v", clientHost, value, err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			dual.workers.Wait()
			if _, err := l.Accept(); !errors.Is(err, net.ErrClosed) {
				t.Fatalf("accept after shutdown: %v", err)
			}
			for _, network := range []string{"tcp4", "tcp6"} {
				host := "127.0.0.1"
				if network == "tcp6" {
					host = "::1"
				}
				rebound, err := net.Listen(network, net.JoinHostPort(host, port))
				if err != nil {
					t.Fatalf("%s port not released: %v", network, err)
				}
				rebound.Close()
			}
		})
	}
}

func TestLoopbackBindFailure(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(map[bool]string{false: "conflict", true: "IPv6 disabled"}[unavailable], func(t *testing.T) {
			var primary net.Listener
			bindErr := error(syscall.EADDRINUSE)
			if unavailable {
				bindErr = syscall.EAFNOSUPPORT
			}
			l, err := listenMySQL("127.0.0.1:0", func(network, address string) (net.Listener, error) {
				if network == "tcp6" {
					return nil, bindErr
				}
				var err error
				primary, err = net.Listen(network, address)
				return primary, err
			})
			if primary == nil {
				t.Fatal("IPv4 listener was not created")
			}
			defer primary.Close()
			if unavailable {
				if err != nil || l != primary {
					t.Fatalf("IPv4 fallback: %v", err)
				}
				l.Close()
			} else if !errors.Is(err, bindErr) || l != nil {
				t.Fatalf("conflict was ignored: %v", err)
			}
			rebound, err := net.Listen("tcp4", primary.Addr().String())
			if err != nil {
				t.Fatalf("IPv4 socket leaked: %v", err)
			}
			rebound.Close()
		})
	}
}

func TestExplicitListenAddressIsPreserved(t *testing.T) {
	for _, address := range []string{"0.0.0.0:3307", "[::]:3307", "[::1]:3307", "127.0.0.2:3307", "192.0.2.1:3307"} {
		calls := 0
		_, err := listenMySQL(address, func(network, actual string) (net.Listener, error) {
			calls++
			if network != "tcp" || actual != address {
				t.Fatalf("rewrote explicit address: %s %s", network, actual)
			}
			return nil, net.ErrClosed
		})
		if calls != 1 || !errors.Is(err, net.ErrClosed) {
			t.Fatalf("address %s: calls=%d err=%v", address, calls, err)
		}
	}
}

func TestLoopbackCloseUnblocksAccept(t *testing.T) {
	requireIPv6Loopback(t)
	l, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	done := make(chan error, 1)
	go func() { _, err := l.Accept(); done <- err }()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not unblock Accept")
	}
	workersDone := make(chan struct{})
	go func() { l.(*loopbackListener).workers.Wait(); close(workersDone) }()
	select {
	case <-workersDone:
	case <-time.After(3 * time.Second):
		t.Fatal("accept workers leaked")
	}
}
