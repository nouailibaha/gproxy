package userspace

import (
	"errors"
	"io"
	"log"
	"net"
	"syscall"
)

func Transport(src, dst net.Conn) {
	_, err := io.Copy(dst, src)

	if err != nil && err != io.EOF {
		if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, net.ErrClosed) {
			return
		}

		log.Printf("Transfer error from %s to %s: %s",
			src.RemoteAddr(), dst.RemoteAddr(), err.Error())
	}
}
