package tcpsplice

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

/*
*
linux.splice() only makes zero copy between a pipe & socket.
In our case we want to achieve a zero copy between 2 sockets:

  - w,r := Pipe()

  - socket1 -> w

  - r <- socket2

    ssize_t splice(int fd_in, off64_t *off_in, int fd_out, off64_t *off_out, size_t len, unsigned int flags);
    https://man7.org/linux/man-pages/man2/splice.2.html
*/
const PIPE_BUFF_SIZE = 16 * 1024

func Transport(src, dst net.Conn) {
	err, srcfd := getFdOutOfConn(src)
	if err != nil {
		log.Fatal("Cant extract fd from srcConn", errors.Unwrap(err))
	}

	err, dstfd := getFdOutOfConn(dst)
	if err != nil {
		log.Fatal("Cant extract fd from dstConn", errors.Unwrap(err))
	}

	r, w, err := os.Pipe()
	if err != nil {
		log.Fatal("Cant create a kernel buffered pipe")
	}
	defer r.Close()
	defer w.Close()

	epfd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		log.Fatal("Failed to create an epoll instance")
	}
	defer unix.Close(epfd)

	event := unix.EpollEvent{
		Events: unix.EPOLLIN,
		// only listen for update on the src conn
		Fd: int32(srcfd),
	}
	unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, srcfd, &event)

	events := make([]unix.EpollEvent, 1)

	for {
		// blocking call - kernel will notifiy when epollin
		n, err := unix.EpollWait(epfd, events, -1)
		if err != nil {
			log.Fatal("Error while waiting for event")
		}

		for i := 0; i < n; i++ {
			n, err := unix.Splice(srcfd, nil, int(w.Fd()), nil, PIPE_BUFF_SIZE, unix.SPLICE_F_NONBLOCK|unix.SPLICE_F_MOVE)
			if err != nil {
				if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, net.ErrClosed) {
					return
				}

				log.Fatal("Cant write to the writer pipe from the srcConn ", err)
			}

			if n == 0 {
				// EOF
				return
			}

			fmt.Println("written bytes.. from src -> pipe:w", n)

			n, err = unix.Splice(int(r.Fd()), nil, dstfd, nil, int(n), unix.SPLICE_F_NONBLOCK|unix.SPLICE_F_MOVE)
			if err != nil {
				if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, net.ErrClosed) {
					return
				}

				log.Fatal("Cant write to the dstfd from the reader pipe ", err)
			}

			fmt.Println("written bytes.. from pipe:r -> dst", n)
		}
	}

}

func getFdOutOfConn(conn net.Conn) (error, int) {
	rawConn, ok := conn.(syscall.Conn)
	if !ok {
		return fmt.Errorf("not a conn"), 0
	}

	rawSyscallConn, err := rawConn.SyscallConn()
	if err != nil {
		return fmt.Errorf("failed to get rawSyscallConn: %v", err), 0
	}

	var fd uintptr
	rawSyscallConn.Control(func(f uintptr) {
		fd = f
	})

	return err, int(fd)
}
