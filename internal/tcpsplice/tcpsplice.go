package tcpsplice

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
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

var PIPE_BUFF_TRANSFER_SIZE = func() int {
	f, err := os.ReadFile("/proc/sys/fs/pipe-max-size")
	if err != nil {
		fmt.Println("Failed to get the pipe max size, falling back to 64 << 10")
		return 64 << 10
	}

	buffSize, err := strconv.Atoi(strings.TrimSuffix(string(f), "\n"))
	if err != nil {
		fmt.Println("Problem occured when converting to an int", err)
		return 64 << 10
	}

	return buffSize
}()

func setPipeSize(fd uintptr) {
	_, err := unix.FcntlInt(uintptr(fd), unix.F_SETPIPE_SZ, PIPE_BUFF_TRANSFER_SIZE)
	if err != nil {
		log.Printf("could not set pipe size to %s: %v", PIPE_BUFF_TRANSFER_SIZE, err)
	}
}

func createAndRegisterEpollEvent(srcfd int) int {
	epfd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		log.Fatal("Failed to create an epoll instance")
	}

	srcEvent := unix.EpollEvent{
		Events: unix.EPOLLIN | unix.EPOLLRDHUP | unix.EPOLLERR | unix.EPOLLHUP,
		// only listen for update on the src conn
		Fd: int32(srcfd),
	}
	unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, srcfd, &srcEvent)

	return epfd
}

func splice(srcfd int, dstfd int) (int, error) {
	n, err := unix.Splice(srcfd, nil, dstfd, nil, PIPE_BUFF_TRANSFER_SIZE, unix.SPLICE_F_MOVE)
	if err != nil {
		if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.EPIPE) {
			fmt.Println("Connection closed, exiting", err)

			return 0, io.EOF
		}

		if errors.Is(err, syscall.EAGAIN) {
			ev := unix.EpollEvent{
				Events: unix.EPOLLOUT,
				Fd:     int32(dstfd),
			}
			epfd, _ := unix.EpollCreate1(0)
			defer unix.Close(epfd)

			unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, dstfd, &ev)
			_, _ = unix.EpollWait(epfd, make([]unix.EpollEvent, 1), -1)

			return splice(srcfd, dstfd)
		}

		fmt.Println("Cant write to the dstfd from srcfd", err)
		return 0, err
	}

	if n == 0 {
		return 0, io.EOF
	}

	return int(n), nil
}

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

	setPipeSize(w.Fd())

	epfd := createAndRegisterEpollEvent(srcfd)
	defer unix.Close(epfd)

	events := make([]unix.EpollEvent, 1)
	for {
		// blocking call - kernel will notifiy

		n, err := unix.EpollWait(epfd, events, -1)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}

			log.Fatal("Error while waiting for event ", err)
		}

		for i := 0; i < n; i++ {
			ev := events[i]

			if ev.Events&(unix.EPOLLERR|unix.EPOLLHUP|unix.EPOLLRDHUP) != 0 {
				// peer closed or error
				unix.EpollCtl(epfd, unix.EPOLL_CTL_DEL, int(ev.Fd), nil)
				unix.Close(int(ev.Fd))

				fmt.Println("Peer closed for fd -> ", ev.Fd)

				continue
			} else {
				n, err := splice(srcfd, int(w.Fd()))
				if err == io.EOF {
					unix.EpollCtl(epfd, unix.EPOLL_CTL_DEL, int(ev.Fd), nil)
					unix.Close(int(ev.Fd))

					fmt.Println("EOF reached, exiting")

					continue
				}

				if err != nil {
					continue
				}
				fmt.Println("written bytes -> from src -> pipe:w", n)

				if err := drainPipeToDst(r, dstfd, int(n), epfd); err != nil {
					if errors.Is(err, io.EOF) {
						unix.EpollCtl(epfd, unix.EPOLL_CTL_DEL, int(ev.Fd), nil)
						unix.Close(int(ev.Fd))

						fmt.Println("EOF reached, exiting")
					}
				}
			}
		}
	}
}

func drainPipeToDst(r *os.File, dstfd int, writtenBytes int, epfd int) error {
	s, err := splice(int(r.Fd()), dstfd)
	if err != nil {
		return err
	}

	fmt.Println("written bytes -> from pipe:r -> dst", s)

	if s < writtenBytes {
		remaining := writtenBytes - int(s)

		log.Printf("Not all bytes written to dst, expected %d, got %d, remaining %d\n", writtenBytes, s, remaining)

		for remaining > 0 {
			m, err := splice(int(r.Fd()), dstfd)

			if m > 0 {
				fmt.Println("drained bytes -> from pipe:r -> dst", m)
				remaining -= int(m)
			}

			if err != nil {
				return err
			}

			if m == 0 {
				fmt.Println("Empty pipe..")
				return nil
			}
		}
	}

	return nil
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
