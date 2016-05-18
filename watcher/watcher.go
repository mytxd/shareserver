package watcher

import (
	"os"
	"syscall"
)

type void struct{}

type Watcher struct {
	fd     int
	Events <-chan Event
	events chan<- Event
	Error  <-chan error
	err    chan<- error
	// done     chan<- void
	watching []syscall.Kevent_t
	paths    map[int]string
	contents map[int]map[string]void // Seriously?
	mounts   map[int]string
}

type Event struct {
	Name  string
	Mount string
}

func New() (*Watcher, error) {
	fd, errno := syscall.Kqueue()
	if fd == -1 {
		return nil, os.NewSyscallError("kqueue", errno)
	}
	ch := make(chan Event)
	errch := make(chan error)
	// done := make(chan void)
	watcher := &Watcher{
		fd, ch, ch, errch, errch, //done,
		make([]syscall.Kevent_t, 0),
		make(map[int]string),
		make(map[int]map[string]void),
		make(map[int]string),
	}

	return watcher, nil
}

func (watcher *Watcher) readDir(fd int) ([]string, error) {
	f, err := os.Open(watcher.paths[fd])
	if err != nil {
		return nil, err
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return nil, err
	}
	return names, nil
}

func (watcher *Watcher) updateDir(fd int, names []string) {
	contents := make(map[string]void)
	for _, name := range names {
		contents[name] = void{}
	}
	watcher.contents[fd] = contents
}

func (watcher *Watcher) Watch(path, mount string) error {
	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		return os.NewSyscallError("open", err)
	}

	watcher.mounts[fd] = mount
	watcher.paths[fd] = path
	names, err := watcher.readDir(fd)
	if err != nil {
		return err
	}
	watcher.updateDir(fd, names)

	ev := syscall.Kevent_t{
		Ident:  uint64(fd),
		Filter: syscall.EVFILT_VNODE,
		Flags:  syscall.EV_ADD | syscall.EV_ENABLE | syscall.EV_ONESHOT,
		Fflags: syscall.NOTE_WRITE,
	}

	watcher.watching = append(watcher.watching, ev)

	return nil
}

func (watcher *Watcher) Close() error {
	// watcher.done <- void{}
	err := syscall.Close(watcher.fd)
	if err != nil {
		return os.NewSyscallError("close", err)
	}

	for _, ev := range watcher.watching {
		err = syscall.Close(int(ev.Ident))
		if err != nil {
			return os.NewSyscallError("close", err)
		}
	}

	return nil
}

func (watcher *Watcher) Run() {
	events := make([]syscall.Kevent_t, 32)
	// timeout := syscall.Timespec{Nsec: 100 * 1e6} // 100ms

	for {
		// select {
		// case <-done:
		//     close(watcher.done)
		//     return
		// default:
		// }

		nev, err := syscall.Kevent(watcher.fd, watcher.watching[:], events, nil)

		if err != nil && err != syscall.EINTR {
			watcher.err <- os.NewSyscallError("kevent", err)
			continue
		}

		dirs := make(map[int]void)

		for i := 0; i < nev; i++ {
			dirs[int(events[i].Ident)] = void{}
		}
		for fd := range dirs {
			names, err := watcher.readDir(fd)
			if err != nil {
				panic(err)
			}
			contents := watcher.contents[fd]
			for _, name := range names {
				_, old := contents[name]
				if !old {
					watcher.events <- Event{name, watcher.mounts[fd]}
				}
			}
			watcher.updateDir(fd, names)
		}
	}
}
