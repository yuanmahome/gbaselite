package server

import (
	"fmt"
	"net"
	"strings"
	"sync"
)

// Listen opens the MySQL endpoint. The default IPv4 loopback address and
// localhost also bind IPv6 loopback on the same port, without exposing LAN
// interfaces. An unavailable IPv6 stack falls back to IPv4; port conflicts fail
// startup rather than letting localhost connect to a different service.
func Listen(address string) (net.Listener, error) {
	return listenMySQL(address, net.Listen)
}

func listenMySQL(address string, listen func(string, string) (net.Listener, error)) (net.Listener, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if host != "127.0.0.1" && !strings.EqualFold(host, "localhost") {
		return listen("tcp", address)
	}
	ipv4, err := listen("tcp4", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return nil, err
	}
	_, port, err = net.SplitHostPort(ipv4.Addr().String())
	if err != nil {
		ipv4.Close()
		return nil, err
	}
	ipv6, err := listen("tcp6", net.JoinHostPort("::1", port))
	if err != nil {
		if ipv6Unavailable(err) {
			return ipv4, nil
		}
		ipv4.Close()
		return nil, fmt.Errorf("listen on IPv6 loopback: %w", err)
	}
	l := &loopbackListener{
		listeners: []net.Listener{ipv4, ipv6},
		results:   make(chan acceptResult),
		done:      make(chan struct{}),
	}
	for _, listener := range l.listeners {
		l.workers.Add(1)
		go l.acceptFrom(listener)
	}
	return l, nil
}

type acceptResult struct {
	connection net.Conn
	err        error
}

type loopbackListener struct {
	listeners []net.Listener
	results   chan acceptResult
	done      chan struct{}
	once      sync.Once
	workers   sync.WaitGroup
}

func (l *loopbackListener) acceptFrom(listener net.Listener) {
	defer l.workers.Done()
	for {
		connection, err := listener.Accept()
		select {
		case l.results <- acceptResult{connection, err}:
		case <-l.done:
			if connection != nil {
				connection.Close()
			}
			return
		}
		if err != nil {
			l.Close()
			return
		}
	}
}

func (l *loopbackListener) Accept() (net.Conn, error) {
	select {
	case <-l.done:
		return nil, net.ErrClosed
	case result := <-l.results:
		select {
		case <-l.done:
			if result.connection != nil {
				result.connection.Close()
			}
			return nil, net.ErrClosed
		default:
			return result.connection, result.err
		}
	}
}

func (l *loopbackListener) Addr() net.Addr { return l.listeners[0].Addr() }

func (l *loopbackListener) Close() error {
	l.once.Do(func() {
		close(l.done)
		for _, listener := range l.listeners {
			_ = listener.Close()
		}
	})
	return nil
}

func listenerAddresses(listener net.Listener) string {
	if l, ok := listener.(*loopbackListener); ok {
		return l.listeners[0].Addr().String() + ", " + l.listeners[1].Addr().String()
	}
	return listener.Addr().String()
}
