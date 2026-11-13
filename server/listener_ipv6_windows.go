package server

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

func ipv6Unavailable(err error) bool {
	return errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EPROTONOSUPPORT) || errors.Is(err, syscall.EADDRNOTAVAIL) ||
		errors.Is(err, windows.WSAEAFNOSUPPORT) || errors.Is(err, windows.WSAEPROTONOSUPPORT) || errors.Is(err, windows.WSAEADDRNOTAVAIL)
}
