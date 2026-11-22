//go:build linux

package simulation

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/netip"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"github.com/jellevandenhooff/gosim/gosimruntime"
	"github.com/jellevandenhooff/gosim/internal/gosimlog"
	"github.com/jellevandenhooff/gosim/internal/simulation/fs"
	"github.com/jellevandenhooff/gosim/internal/simulation/network"
	"github.com/jellevandenhooff/gosim/internal/simulation/syscallabi"
)

// LinuxOS implements gosim's versions of Linux system calls.
type LinuxOS struct {
	dispatcher syscallabi.Dispatcher

	simulation *Simulation

	machine *Machine

	mu       sync.Mutex
	shutdown bool

	files map[int]interface{}
	// dupFds are the descriptors at or above nextFd that F_DUPFD handed
	// out, closed or not, which allocFd skips.
	dupFds map[int]bool
	nextFd int

	mmaps map[uintptr]*fs.Mmap

	workdirInode int
}

func NewLinuxOS(simulation *Simulation, machine *Machine, dispatcher syscallabi.Dispatcher) *LinuxOS {
	// TODO: initialize the working directory more elegantly
	workdirInode, err := machine.filesystem.Getdirinode(fs.RootInode, "/app")
	if err != nil {
		panic(err)
	}

	return &LinuxOS{
		dispatcher: dispatcher,

		simulation: simulation,

		machine: machine,

		files:  make(map[int]interface{}),
		dupFds: make(map[int]bool),
		nextFd: 5,

		mmaps: make(map[uintptr]*fs.Mmap),

		workdirInode: workdirInode,
	}
}

func (l *LinuxOS) doShutdown() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.shutdown = true

	// nil out pointers to catch any use after shutdown
	l.machine = nil
	l.files = nil
}

func logf(format string, args ...any) {
	if logSyscalls {
		slog.Info(fmt.Sprintf(format, args...), "traceKind", "syscall")
	}
}

func logSyscallEntry(name string, invocation *syscallabi.Syscall, attrs ...any) {
	attrs = append(attrs, "step", invocation.Step, "traceKind", "syscall")
	slog.Info("call "+name, attrs...)
}

func logSyscallExit(name string, invocation *syscallabi.Syscall, attrs ...any) {
	attrs = append(attrs, "relatedStep", invocation.Step, "traceKind", "syscall")
	slog.Info("ret  "+name, attrs...)
}

func byteDataAttr(key string, value []byte) slog.Attr {
	return slog.String(key, "base64:"+base64.StdEncoding.EncodeToString(value))
}

func fdAttr(name string, fd int) slog.Attr {
	switch fd {
	case _AT_FDCWD:
		return slog.String(name, "AT_FDCWD")
	default:
		return slog.Int(name, fd)
	}
}

func addrAttr(addr unsafe.Pointer, addrlen Socklen) slog.Attr {
	parsedAddr, parseErr := readAddr(addr, addrlen)
	if parseErr != 0 {
		return slog.Any("addr", parseErr)
	} else {
		return slog.Any("addr", parsedAddr)
	}
}

func int8ArrayToString(key string, value []int8) slog.Attr {
	n := 0
	for i, c := range value {
		if c == 0 {
			n = i
			break
		}
	}
	b := make([]byte, n)
	for i := range n {
		b[i] = byte(value[i])
	}
	return slog.String(key, string(b))
}

// logfFor logs the given format and args on the goroutine from the passed
// invocation.
func (l *LinuxOS) logfFor(invocation *syscallabi.Syscall, format string, args ...any) {
	if !logSyscalls {
		return
	}

	if invocation.Step == 0 {
		return
	}

	msg := fmt.Sprintf(format, args...)
	slog.Info(msg, "relatedStep", invocation.Step, "traceKind", "syscall")

	/*
		r := slog.NewRecord(time.Now(), slog.LevelInfo, msg, invocation.PC)
		r.Add("machine", l.machine.label, "goroutine", invocation.Goroutine, "step", gosimruntime.Step(), "traceKind", "syscall")
		if gosimruntime.TraceStack.Enabled() {
			// TODO: log invocation from its goroutine instead and skip this trickery?
			// TODO: stick stacktrace on invocation instead?
			var pcs [128]uintptr
			n := gosimruntime.GetStacktraceFor(invocation.Goroutine, pcs[:])
			r.AddAttrs(gosimlog.StackFor(pcs[:n], invocation.PC))
		}

		l.simulation.rawLogger.Handler().Handle(context.TODO(), r)
	*/
}

// used to be...
// //go:nocheckptr
// //go:uintptrescapes
func RawSyscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno) {
	if IsHandledSyscall(trap) {
		// A syscall the simulation handles arrives here raw, from code
		// that calls RawSyscall6 itself rather than through package
		// syscall's wrappers, which gosim renames. It fails with ENOSYS,
		// as any syscall the simulation does not handle, for the caller
		// to deal with.
		logf("raw syscall %s (%d), which the simulation handles only through package syscall", SyscallName(trap), trap)
		return 0, 0, syscall.ENOSYS
	}

	logf("unsupported syscall %s (%d) %d %d %d %d %d %d", SyscallName(trap), trap, a1, a2, a3, a4, a5, a6)

	return 0, 0, syscall.ENOSYS
}

// maxFds is the limit of a machine's file descriptors, Linux's default hard
// limit of open files.
const maxFds = 1 << 20

// lowestFreeFd takes the lowest free file descriptor at or above min and
// below maxFds, as F_DUPFD does, and reports false if there is none. 0 to
// 2 are the standard files'. The caller puts the descriptor in files.
func (l *LinuxOS) lowestFreeFd(min int) (int, bool) {
	for fd := max(min, 3); fd < maxFds; fd++ {
		if _, ok := l.files[fd]; !ok {
			if fd >= l.nextFd {
				l.dupFds[fd] = true
			}
			return fd, true
		}
	}
	return 0, false
}

// allocFd returns a new file descriptor for open, socket and accept: the
// next one from nextFd up that no one has had, so that a descriptor
// closed is not handed out again by them, which makes a use after close
// fail. It skips the ones F_DUPFD took above nextFd (dupFds). The caller
// puts the descriptor in files.
func (l *LinuxOS) allocFd() int {
	for {
		fd := l.nextFd
		l.nextFd++
		taken := l.dupFds[fd]
		delete(l.dupFds, fd)
		if !taken {
			return fd
		}
	}
}

func (l *LinuxOS) PollOpen(fd int, desc syscallabi.ValueView[syscallabi.PollDesc], invocation *syscallabi.Syscall) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		// XXX: what do we return here? shouldn't matter?
		return 0
	}

	f := l.files[fd]
	if socket, ok := f.(*Socket); ok {
		if socket.Stream != nil {
			l.machine.netstack.RegisterStreamPoller((*syscallabi.PollDesc)(desc.UnsafePointer()), socket.Stream)
		}
		if socket.Listener != nil {
			l.machine.netstack.RegisterListenerPoller((*syscallabi.PollDesc)(desc.UnsafePointer()), socket.Listener)
		}
	}
	return 0
}

func (customSyscallLogger) LogEntryPollOpen(fd int, desc *syscallabi.PollDesc, syscall *syscallabi.Syscall) {
	// logSyscallEntry("PollOpen", syscall, "fd", fd)
}

func (customSyscallLogger) LogExitPollOpen(fd int, desc *syscallabi.PollDesc, syscall *syscallabi.Syscall, code int) {
	// logSyscallExit("PollOpen", syscall, "code", code)
}

func (l *LinuxOS) PollClose(fd int, desc syscallabi.ValueView[syscallabi.PollDesc], invocation *syscallabi.Syscall) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		// XXX: what do we return here? shouldn't matter?
		return 0
	}

	f := l.files[fd]
	if socket, ok := f.(*Socket); ok {
		if socket.Stream != nil {
			l.machine.netstack.DeregisterStreamPoller((*syscallabi.PollDesc)(desc.UnsafePointer()), socket.Stream)
		}
		if socket.Listener != nil {
			l.machine.netstack.DeregisterListenerPoller((*syscallabi.PollDesc)(desc.UnsafePointer()), socket.Listener)
		}
	}
	return 0
}

func (customSyscallLogger) LogEntryPollClose(fd int, desc *syscallabi.PollDesc, syscall *syscallabi.Syscall) {
	// logSyscallEntry("PollClose", syscall, "fd", fd)
}

func (customSyscallLogger) LogExitPollClose(fd int, desc *syscallabi.PollDesc, syscall *syscallabi.Syscall, code int) {
	// logSyscallExit("PollClose", syscall, "code", code)
}

type (
	RawSockaddrAny = syscall.RawSockaddrAny
	Utsname        = syscall.Utsname
	Stat_t         = syscall.Stat_t
	Statfs_t       = syscall.Statfs_t
)

type Socklen uint32

const (
	_AT_FDCWD            = -0x64
	_AT_REMOVEDIR        = 0x200
	_AT_SYMLINK_NOFOLLOW = 0x100
)

// TODO: make functions instead return Errno again?

// for disk:
// to sim
// - random op errors
// - latency
// - writes get split in parts

// behavior tests
// - assert that behavior shows up in (some) runs
// - assert that behavior never shows up
// - assert that we have all possible behavior (exact)

// brokenness that might happen:
// - file entries are not are persisted until fsync on the directory [done]
// - writes to a file might be reordered until fsync [done]
// - writes to different files might be reordered [done]
// - an appended-to file might have zeroes [done]
// - after truncate a file might still have old data afterwards (zeroes not guaranteed)

// XXX:
// - (later:) alert on cross-machine interactions (eg. chan, memory, ...?)

type OsFile struct {
	inode               int
	name                string
	pos                 int64
	flagRead, flagWrite bool
	// flagAppend is O_APPEND: every write goes to the end of the file.
	flagAppend bool

	didReaddir bool // XXX JANK
}

type Socket struct {
	IsBound   bool
	BoundAddr netip.AddrPort

	Listener *network.Listener
	Stream   *network.Stream

	RecvBuffer []byte

	// pending socket
	// tcp listener
	// file
	// tcp conn
}

// dirInodeForFd returns the directory that paths relative to dirfd start
// from: the working directory for AT_FDCWD, or an open directory, as
// os.File.ReadDir and os.RemoveAll use.
func (l *LinuxOS) dirInodeForFd(dirfd int) (int, error) {
	if dirfd == _AT_FDCWD {
		return l.workdirInode, nil
	}
	f, ok := l.files[dirfd].(*OsFile)
	if !ok {
		return 0, syscall.EBADF
	}
	if !l.isDir(f.inode) {
		return 0, syscall.ENOTDIR
	}
	return f.inode, nil
}

func (l *LinuxOS) SysOpenat(dirfd int, path string, flags int, mode uint32, invocation *syscallabi.Syscall) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return 0, syscall.EINVAL
	}

	dirInode, err := l.dirInodeForFd(dirfd)
	if err != nil {
		return 0, err
	}

	// just get rid of this
	flags &= ^syscall.O_CLOEXEC
	// There are no symbolic links, so O_NOFOLLOW changes nothing:
	// os.RemoveAll opens directories with it.
	flags &= ^syscall.O_NOFOLLOW

	// O_DIRECTORY fails unless path is a directory. Go's os package sets it
	// when it opens a directory to read it.
	if flags&syscall.O_DIRECTORY != 0 {
		flags &= ^syscall.O_DIRECTORY
		stat, err := l.machine.filesystem.Stat(dirInode, path)
		if err != nil {
			return 0, err
		}
		if !stat.IsDir {
			return 0, syscall.ENOTDIR
		}
	}

	// TODO: some rules about paths; component length; total length; allow characters?
	// TODO: check mode

	const flagSupported = os.O_RDONLY | os.O_WRONLY | os.O_RDWR |
		os.O_CREATE | os.O_EXCL | os.O_TRUNC | os.O_APPEND

	if flags&(^flagSupported) != 0 {
		return 0, syscall.EINVAL // XXX?
	}

	var flagRead, flagWrite bool
	switch flags & 0x3 {
	case os.O_WRONLY:
		flagWrite = true
	case os.O_RDWR:
		flagRead = true
		flagWrite = true
	case os.O_RDONLY:
		flagRead = true
	default:
		return 0, syscall.EINVAL // XXX?
	}

	flagAppend := flags&os.O_APPEND != 0
	inode, err := l.machine.filesystem.OpenFile(dirInode, path, flags&^os.O_APPEND)
	if err != nil {
		return 0, err
	}

	// TODO: add reference here? or in OpenFile? help this is strange.

	// TODO: this pos is wrong for writing maybe? what should happen when you
	// open a file that already exists and start writing to it?
	f := &OsFile{
		name:       path,
		inode:      inode,
		pos:        0,
		flagRead:   flagRead,
		flagWrite:  flagWrite,
		flagAppend: flagAppend,
	}

	fd := l.allocFd()
	l.files[fd] = f

	return fd, nil
}

var openatFlags = &gosimlog.BitflagFormatter{
	Choices: []gosimlog.BitflagChoice{
		{
			Mask: 0x3,
			Values: map[int]string{
				syscall.O_RDWR:   "O_RDWR",
				syscall.O_RDONLY: "O_RDONLY",
				syscall.O_WRONLY: "O_WRONLY",
			},
		},
	},
	Flags: []gosimlog.BitflagValue{
		{
			Value: syscall.O_TRUNC,
			Name:  "O_TRUNC",
		},
		{
			Value: syscall.O_CREAT,
			Name:  "O_CREAT",
		},
		{
			Value: syscall.O_APPEND,
			Name:  "O_APPEND",
		},
		{
			Value: syscall.O_EXCL,
			Name:  "O_EXCL",
		},
		{
			Value: syscall.O_CLOEXEC,
			Name:  "O_CLOEXEC",
		},
		{
			Value: syscall.O_DIRECTORY,
			Name:  "O_DIRECTORY",
		},
	},
}

func (customSyscallLogger) LogEntrySysOpenat(dirfd int, path string, flags int, mode uint32, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysOpenat", syscall, fdAttr("dirfd", dirfd), "path", path, "flags", openatFlags.Format(flags), "mode", fmt.Sprintf("%O", mode))
}

func (customSyscallLogger) LogExitSysOpenat(dirfd int, path string, flags int, mode uint32, syscall *syscallabi.Syscall, fd int, err error) {
	logSyscallExit("SysOpenat", syscall, "fd", fd, "err", err)
}

func (l *LinuxOS) SysRenameat(olddirfd int, oldpath string, newdirfd int, newpath string, invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	oldInode, err := l.dirInodeForFd(olddirfd)
	if err != nil {
		return err
	}
	newInode, err := l.dirInodeForFd(newdirfd)
	if err != nil {
		return err
	}

	if err := l.machine.filesystem.Rename(oldInode, oldpath, newInode, newpath); err != nil {
		if err == syscall.ENOENT {
			return syscall.ENOENT
		}
		return syscall.EINVAL // XXX?
	}

	return nil
}

func (customSyscallLogger) LogEntrySysRenameat(olddirfd int, oldpath string, newdirfd int, newpath string, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysRenameat", syscall, fdAttr("olddirfd", olddirfd), "oldpath", oldpath, fdAttr("newdirfd", newdirfd), "newpath", newpath)
}

func (customSyscallLogger) LogExitSysRenameat(diolddirfd int, oldpath string, newdirfd int, newpath string, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysRenameat", syscall, "err", err)
}

func (l *LinuxOS) SysGetdents64(fd int, data syscallabi.ByteSliceView, invocation *syscallabi.Syscall) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return 0, syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return 0, syscall.EBADFD
	}

	f, ok := fdInternal.(*OsFile)
	if !ok {
		return 0, syscall.ENOTDIR
	}

	if f.didReaddir {
		return 0, nil
	}

	// TODO: make this iteration work with some kind of pointer
	entries, err := l.machine.filesystem.ReadDir(f.inode)
	if err != nil {
		return 0, syscall.EINVAL // XXX?
	}

	var localbuffer [4096]byte

	retn := 0
	for _, entry := range entries {
		name := entry.Name

		inode := int64(1)
		offset := int64(1)
		reclen := int(19) + len(name) + 1

		if reclen > len(localbuffer) {
			panic("help")
		}
		if retn+reclen > data.Len() {
			break
		}

		buffer := localbuffer[:reclen]
		typ := syscall.DT_REG
		if entry.IsDir {
			typ = syscall.DT_DIR
		}

		binary.LittleEndian.PutUint64(buffer[0:8], uint64(inode))
		binary.LittleEndian.PutUint64(buffer[8:16], uint64(offset))
		binary.LittleEndian.PutUint16(buffer[16:18], uint16(reclen))
		buffer[18] = byte(typ)
		copy(buffer[19:], name)
		buffer[19+len(name)] = 0

		data.Slice(retn, retn+reclen).Write(buffer)
		retn += reclen
	}

	f.didReaddir = true

	return retn, nil
}

func (customSyscallLogger) LogEntrySysGetdents64(fd int, buf []byte, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysGetdents64", syscall, "fd", fd, "len(buf)", len(buf))
}

type Dentry struct {
	Inode  uint64
	Offset uint64
	Reclen uint16
	Type   string
	Name   string
}

var dtType = &gosimlog.BitflagFormatter{
	Choices: []gosimlog.BitflagChoice{
		{
			Mask: 0xf,
			Values: map[int]string{
				syscall.DT_REG: "DT_REG",
				syscall.DT_DIR: "DT_DIR",
			},
		},
	},
}

func (customSyscallLogger) LogExitSysGetdents64(fd int, buf []byte, syscall *syscallabi.Syscall, n int, err error) {
	var entries []*Dentry
	buf = buf[:n]
	for len(buf) > 19 {
		var entry Dentry
		entry.Inode = binary.LittleEndian.Uint64(buf[0:8])
		entry.Offset = binary.LittleEndian.Uint64(buf[8:16])
		entry.Reclen = binary.LittleEndian.Uint16(buf[16:18])
		entry.Type = dtType.Format(int(buf[18]))
		nameLen := entry.Reclen - 19 - 1
		entry.Name = string(buf[19 : 19+nameLen])
		entries = append(entries, &entry)
		buf = buf[entry.Reclen:]
	}
	logSyscallExit("SysGetdents64", syscall, "n", n, "err", err, "entries", entries)
}

func (l *LinuxOS) SysWrite(fd int, data syscallabi.ByteSliceView, invocation *syscallabi.Syscall) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return 0, syscall.EINVAL
	}

	if stdio, ok := l.stdioFd(fd); ok && (stdio == syscall.Stdout || stdio == syscall.Stderr) {
		buf := make([]byte, data.Len())
		data.Read(buf)

		buf = bytes.TrimSuffix(buf, []byte("\n"))

		var source string
		if stdio == syscall.Stdout {
			source = "stdout"
		} else {
			source = "stderr"
		}

		l.simulation.rawLogger.Info(
			string(buf), "method", source, "machine", l.machine.label, "goroutine", invocation.Goroutine, "step", gosimruntime.Step())

		// slog.Info(string(buf), "method", source, "from", l.machine.label)
		return data.Len(), nil
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		l.logfFor(invocation, "write %d badfd", fd)
		return 0, syscall.EBADFD
	}

	switch f := fdInternal.(type) {
	case *OsFile:
		if !f.flagWrite {
			return 0, syscall.EBADFD
		}

		if f.flagAppend {
			stat, err := l.machine.filesystem.Statfd(f.inode)
			if err != nil {
				return 0, syscall.EBADFD
			}
			f.pos = stat.Size
		}
		retn := l.machine.filesystem.Write(f.inode, f.pos, data)
		f.pos += int64(retn)

		return retn, nil

	case *Socket:
		if f.Stream == nil {
			return 0, syscall.EBADFD
		}

		retn, err := l.machine.netstack.StreamSend(f.Stream, data)
		if err != nil {
			if err == syscall.EWOULDBLOCK {
				return retn, syscall.EWOULDBLOCK
			}
			if err == network.ErrStreamClosed {
				return retn, syscall.EPIPE
			}
			return retn, syscall.EBADFD
		}

		return retn, nil

	default:
		return 0, syscall.EBADFD
	}
}

func (customSyscallLogger) LogEntrySysWrite(fd int, p []byte, syscall *syscallabi.Syscall) {
	// TODO: format these []byte attrs nicer: handle both ascii and binary gracefully
	logSyscallEntry("SysWrite", syscall, "fd", fd, byteDataAttr("p", p))
}

func (customSyscallLogger) LogExitSysWrite(fd int, p []byte, syscall *syscallabi.Syscall, n int, err error) {
	logSyscallExit("SysWrite", syscall, "n", n, "err", err)
}

func (l *LinuxOS) SysRead(fd int, data syscallabi.ByteSliceView, invocation *syscallabi.Syscall) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return 0, syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return 0, syscall.EBADFD
	}

	switch f := fdInternal.(type) {
	case *OsFile:
		if !f.flagRead {
			return 0, syscall.EBADFD // XXX??
		}
		if l.isDir(f.inode) {
			return 0, syscall.EISDIR
		}

		retn := l.machine.filesystem.Read(f.inode, f.pos, data)
		f.pos += int64(retn)

		return retn, nil

	case *Socket:
		if f.Stream == nil {
			return 0, syscall.EBADFD
		}

		retn, err := l.machine.netstack.StreamRecv(f.Stream, data)
		if err != nil {
			if err == syscall.EWOULDBLOCK {
				return 0, syscall.EWOULDBLOCK
			}
			if err == network.ErrStreamClosed {
				return 0, syscall.EPIPE
			}
			return 0, syscall.EBADFD
		}

		return retn, nil

	default:
		return 0, syscall.EBADFD
	}
}

func (customSyscallLogger) LogEntrySysRead(fd int, p []byte, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysRead", syscall, "fd", fd, "len(p)", len(p))
}

func (customSyscallLogger) LogExitSysRead(fd int, p []byte, syscall *syscallabi.Syscall, n int, err error) {
	logSyscallExit("SysRead", syscall, "n", n, "err", err, byteDataAttr("p", p[:n]))
}

func (l *LinuxOS) SysFallocate(fd int, mode uint32, off int64, len int64, invocation *syscallabi.Syscall) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return syscall.EBADFD
	}

	f, ok := fdInternal.(*OsFile)
	if !ok {
		return syscall.EBADFD
	}

	l.logfFor(invocation, "fallocate %d %d %d %d", fd, mode, off, len)

	if mode != 0 {
		return syscall.EINVAL
	}

	if off != 0 {
		return syscall.EINVAL
	}

	stat, err := l.machine.filesystem.Statfd(f.inode)
	if err != nil {
		// huh
		return err
	}

	if len > stat.Size {
		l.machine.filesystem.Truncate(f.inode, int(len))
	}

	return nil
}

var lseekWhence = &gosimlog.BitflagFormatter{
	Choices: []gosimlog.BitflagChoice{
		{
			Mask: 0x3,
			Values: map[int]string{
				io.SeekCurrent: "SEEK_CUR",
				io.SeekEnd:     "SEEK_END",
				io.SeekStart:   "SEEK_SET",
			},
		},
	},
}

func (l *LinuxOS) SysLseek(fd int, offset int64, whence int, invocation *syscallabi.Syscall) (off int64, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return 0, syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return 0, syscall.EBADFD
	}

	f, ok := fdInternal.(*OsFile)
	if !ok {
		return 0, syscall.EBADFD
	}

	var newPos int64

	switch whence {
	case io.SeekCurrent:
		newPos = f.pos + offset
	case io.SeekEnd:
		stat, err := l.machine.filesystem.Statfd(f.inode)
		if err != nil {
			// huh
			return 0, err
		}
		newPos = stat.Size + offset
	case io.SeekStart:
		newPos = offset
	default:
		return 0, syscall.EINVAL
	}

	if newPos < 0 {
		return 0, syscall.EINVAL
	}

	f.pos = newPos

	return f.pos, nil
}

func (customSyscallLogger) LogEntrySysLseek(fd int, offset int64, whence int, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysLseek", syscall, "fd", fd, "offset", offset, "whence", lseekWhence.Format(whence))
}

func (customSyscallLogger) LogExitSysLseek(fd int, offset int64, whence int, syscall *syscallabi.Syscall, off int64, err error) {
	logSyscallExit("SysLseek", syscall, "off", off, "err", err)
}

func (l *LinuxOS) SysPwrite64(fd int, data syscallabi.ByteSliceView, offset int64, invocation *syscallabi.Syscall) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return 0, syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return 0, syscall.EBADFD
	}

	f, ok := fdInternal.(*OsFile)
	if !ok {
		return 0, syscall.EBADFD
	}

	if !f.flagWrite {
		return 0, syscall.EBADFD
	}

	retn := l.machine.filesystem.Write(f.inode, offset, data)

	return retn, nil
}

func (customSyscallLogger) LogEntrySysPwrite64(fd int, p []byte, offset int64, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysPwrite64", syscall, "fd", fd, "offset", offset, byteDataAttr("p", p))
}

func (customSyscallLogger) LogExitSysPwrite64(fd int, p []byte, offset int64, syscall *syscallabi.Syscall, n int, err error) {
	logSyscallExit("SysPwrite64", syscall, "n", n, "err", err)
}

func (l *LinuxOS) SysPread64(fd int, data syscallabi.ByteSliceView, offset int64, invocation *syscallabi.Syscall) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return 0, syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return 0, syscall.EBADFD
	}

	f, ok := fdInternal.(*OsFile)
	if !ok {
		return 0, syscall.EBADFD
	}

	if !f.flagRead {
		return 0, syscall.EBADFD // XXX??
	}

	if l.isDir(f.inode) {
		return 0, syscall.EISDIR
	}
	retn := l.machine.filesystem.Read(f.inode, offset, data)

	return retn, nil
}

func (customSyscallLogger) LogEntrySysPread64(fd int, p []byte, offset int64, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysPread64", syscall, "fd", fd, "offset", offset, "len(p)", len(p))
}

func (customSyscallLogger) LogExitSysPread64(fd int, p []byte, offset int64, syscall *syscallabi.Syscall, n int, err error) {
	logSyscallExit("SysPread64", syscall, "n", n, "err", err, byteDataAttr("p", p[:n]))
}

func (l *LinuxOS) SysFsync(fd int, invocation *syscallabi.Syscall) error {
	// XXX: janky way to crash on sync
	l.machine.sometimesCrashOnSyncMu.Lock()
	maybeCrash := l.machine.sometimesCrashOnSync
	l.machine.sometimesCrashOnSyncMu.Unlock()
	if maybeCrash && rand.Intn(100) > 90 {
		if rand.Int()&1 == 0 {
			// XXX: include fd's file somehow???
			slog.Info("crashing machine before sync", "machine", l.machine.label)
			l.simulation.mu.Lock()
			defer l.simulation.mu.Unlock()
			l.simulation.stopMachine(l.machine, false)
			return syscall.EINTR // XXX whatever this should never be seen
		} else {
			defer func() {
				slog.Info("crashing machine after sync", "machine", l.machine.label)
				l.simulation.mu.Lock()
				defer l.simulation.mu.Unlock()
				l.simulation.stopMachine(l.machine, false)
			}()
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return syscall.EBADFD
	}

	f, ok := fdInternal.(*OsFile)
	if !ok {
		return syscall.EBADFD
	}

	l.machine.filesystem.Sync(f.inode)

	return nil
}

func (customSyscallLogger) LogEntrySysFsync(fd int, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysFsync", syscall, "fd", fd)
}

func (customSyscallLogger) LogExitSysFsync(fd int, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysFsync", syscall, "err", err)
}

func (l *LinuxOS) SysFdatasync(fd int, invocation *syscallabi.Syscall) error {
	// TODO: sync only some subset instead?
	return l.SysFsync(fd, invocation)
}

func (customSyscallLogger) LogEntrySysFdatasync(fd int, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysFdatasync", syscall, "fd", fd)
}

func (customSyscallLogger) LogExitSysFdatasync(fd int, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysFdatasync", syscall, "err", err)
}

func fillStat(view syscallabi.ValueView[syscall.Stat_t], in fs.StatResp) {
	var out syscall.Stat_t
	out.Ino = uint64(in.Inode)
	if in.IsDir {
		out.Mode = syscall.S_IFDIR
	} else {
		out.Mode = syscall.S_IFREG
	}
	out.Size = in.Size
	view.Set(out)
}

func (l *LinuxOS) SysFstat(fd int, statBuf syscallabi.ValueView[syscall.Stat_t], invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	l.logfFor(invocation, "fstat %d", fd)

	if _, isStdio := l.stdioFd(fd); isStdio {
		// The standard files are a terminal's character device, which
		// programs check before they write colors to it.
		var stat syscall.Stat_t
		stat.Mode = syscall.S_IFCHR | 0o620
		stat.Blksize = 1024
		statBuf.Set(stat)
		return nil
	}
	fdInternal, ok := l.files[fd]
	if !ok {
		return syscall.EBADFD
	}

	// XXX: handle dir
	f, ok := fdInternal.(*OsFile)
	if !ok {
		return syscall.EBADFD
	}

	fsStat, err := l.machine.filesystem.Statfd(f.inode)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return syscall.ENOENT
		}
		return syscall.EINVAL // XXX?
	}

	fillStat(statBuf, fsStat)

	return nil
}

// arm64
func (l *LinuxOS) SysFstatat(dirfd int, path string, statBuf syscallabi.ValueView[syscall.Stat_t], flags int, invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	dirInode, err := l.dirInodeForFd(dirfd)
	if err != nil {
		return err
	}

	// There are no symbolic links, so not following them changes nothing:
	// os.Lstat passes AT_SYMLINK_NOFOLLOW.
	if flags&^_AT_SYMLINK_NOFOLLOW != 0 {
		return syscall.EINVAL // XXX?
	}

	l.logfFor(invocation, "fstatat %d %s %d", dirfd, path, flags)

	fsStat, err := l.machine.filesystem.Stat(dirInode, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return syscall.ENOENT
		}
		return syscall.EINVAL // XXX?
	}

	fillStat(statBuf, fsStat)

	return nil
}

// amd64
func (l *LinuxOS) SysNewfstatat(dirfd int, path string, statBuf syscallabi.ValueView[syscall.Stat_t], flags int, invocation *syscallabi.Syscall) error {
	return l.SysFstatat(dirfd, path, statBuf, flags, invocation)
}

var unlinkatFlags = &gosimlog.BitflagFormatter{
	Flags: []gosimlog.BitflagValue{
		{
			Value: _AT_REMOVEDIR,
			Name:  "AT_REMOVEDIR",
		},
	},
}

func (l *LinuxOS) SysUnlinkat(dirfd int, path string, flags int, invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	dirInode, err := l.dirInodeForFd(dirfd)
	if err != nil {
		return err
	}

	switch flags {
	case 0:
		if err := l.machine.filesystem.Remove(dirInode, path, false); err != nil {
			return err
		}
		return nil
	case _AT_REMOVEDIR:
		if err := l.machine.filesystem.Remove(dirInode, path, true); err != nil {
			return err
		}
		return nil
	default:
		return syscall.EINVAL
	}
}

func (customSyscallLogger) LogEntrySysUnlinkat(dirfd int, path string, flags int, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysUnlinkat", syscall, fdAttr("dirfd", dirfd), "path", path, "flags", unlinkatFlags.Format(flags))
}

func (customSyscallLogger) LogExitSysUnlinkat(dirfd int, path string, flags int, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysUnlinkat", syscall, "err", err)
}

func (l *LinuxOS) SysFtruncate(fd int, n int64, invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return syscall.EBADFD
	}

	f, ok := fdInternal.(*OsFile)
	if !ok {
		return syscall.EBADFD
	}

	if !f.flagWrite {
		return syscall.EBADFD // XXX?
	}

	l.machine.filesystem.Truncate(f.inode, int(n))

	return nil
}

func (customSyscallLogger) LogEntrySysFtruncate(fd int, length int64, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysFtruncate", syscall, "fd", fd, "length", length)
}

func (customSyscallLogger) LogExitSysFtruncate(fd int, length int64, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysFtruncate", syscall, "err", err)
}

func (l *LinuxOS) SysClose(fd int, invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return syscall.EBADFD
	}

	switch fdInternal := fdInternal.(type) {
	case *OsFile:
		if err := l.machine.filesystem.CloseFile(fdInternal.inode); err != nil {
			// XXX?
			return syscall.EBADFD
		}

	case *stdioCopy:

	case *Socket:
		// Closing a socket closes its stream or listener, so the peer
		// sees the end of the stream.
		if fdInternal.Stream != nil {
			if err := l.machine.netstack.StreamClose(fdInternal.Stream); err != nil && err != network.ErrStreamClosed {
				return syscall.EBADFD
			}
		}
		if fdInternal.Listener != nil {
			if err := l.machine.netstack.CloseListener(fdInternal.Listener); err != nil && err != network.ErrListenerClosed {
				return syscall.EBADFD
			}
		}

	default:
		return syscall.EBADFD
	}

	delete(l.files, fd)

	return nil
}

func (customSyscallLogger) LogEntrySysClose(fd int, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysClose", syscall, "fd", fd)
}

func (customSyscallLogger) LogExitSysClose(fd int, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysClose", syscall, "err", err)
}

func (l *LinuxOS) SysSocket(net, flags, proto int, invocation *syscallabi.Syscall) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return 0, syscall.EINVAL
	}

	// handle sock_posix func (p *ipStackCapabilities) probe()

	// ipv4 close on exec non blocking tcp streams ONLY
	if net != syscall.AF_INET {
		return 0, syscall.EINVAL // XXX?
	}
	if flags != syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC|syscall.SOCK_NONBLOCK {
		return 0, syscall.EINVAL // XXX?
	}
	if proto != 0 { // XXX: no ipv6
		return 0, syscall.EINVAL // XXX?
	}

	fd := l.allocFd()
	l.files[fd] = &Socket{}

	return fd, nil
}

var socketNet = &gosimlog.BitflagFormatter{
	Choices: []gosimlog.BitflagChoice{
		{
			Mask: 0xff,
			Values: map[int]string{
				syscall.AF_INET: "AF_INET",
			},
		},
	},
}

var socketFlags = &gosimlog.BitflagFormatter{
	Choices: []gosimlog.BitflagChoice{
		{
			Mask: 0xf,
			Values: map[int]string{
				syscall.SOCK_STREAM: "SOCK_STREAM",
				syscall.SOCK_PACKET: "SOCK_PACKET",
			},
		},
	},
	Flags: []gosimlog.BitflagValue{
		{
			Value: syscall.SOCK_NONBLOCK,
			Name:  "SOCK_NONBLOCK",
		},
		{
			Value: syscall.SOCK_CLOEXEC,
			Name:  "SOCK_CLOEXEC",
		},
	},
}

var socketProto = &gosimlog.BitflagFormatter{
	Choices: []gosimlog.BitflagChoice{
		{
			Mask: 0xff,
			Values: map[int]string{
				syscall.IPPROTO_IP:   "IPPROTO_IP",
				syscall.IPPROTO_IPV6: "IPPROTO_IPV6",
			},
		},
	},
}

func (customSyscallLogger) LogEntrySysSocket(net, flags, proto int, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysSocket", syscall, "net", socketNet.Format(net), "flags", socketFlags.Format(flags), "proto", socketProto.Format(proto))
}

func (customSyscallLogger) LogExitSysSocket(net, flags, proto int, syscall *syscallabi.Syscall, fd int, err error) {
	logSyscallExit("SysSocket", syscall, "fd", fd, "err", err)
}

func (l *LinuxOS) SysBind(fd int, addrPtr unsafe.Pointer, addrlen Socklen, invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return syscall.EBADFD
	}

	sock, ok := fdInternal.(*Socket)
	if !ok {
		return syscall.ENOTSOCK
	}

	if sock.IsBound {
		return syscall.EINVAL
	}

	addr, err := readAddr(addrPtr, addrlen)
	if err != 0 {
		return err
	}

	switch {
	case addr.Addr().Is4():
	default:
		return syscall.EINVAL
	}

	sock.IsBound = true
	sock.BoundAddr = addr
	return nil
}

func (customSyscallLogger) LogEntrySysBind(s int, addr unsafe.Pointer, addrlen Socklen, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysBind", syscall, "fd", s, addrAttr(addr, addrlen))
}

func (customSyscallLogger) LogExitSysBind(s int, addr unsafe.Pointer, addrlen Socklen, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysBind", syscall, "err", err)
}

func (l *LinuxOS) SysListen(fd, backlog int, invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return syscall.EBADFD
	}

	sock, ok := fdInternal.(*Socket)
	if !ok {
		return syscall.ENOTSOCK
	}

	if !sock.IsBound {
		return syscall.EBADFD
	}

	if sock.Listener != nil {
		return syscall.EBADFD
	}

	// XXX
	listener, err := l.machine.netstack.OpenListener(sock.BoundAddr.Port())
	if err != nil {
		return syscall.EINVAL
	}
	sock.Listener = listener
	// A listener bound to port 0 got a new port.
	sock.BoundAddr = netip.AddrPortFrom(sock.BoundAddr.Addr(), listener.Port())

	return nil
}

func (customSyscallLogger) LogEntrySysListen(s int, n int, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysListen", syscall, "fd", s, "n", n)
}

func (customSyscallLogger) LogExitSysListen(s int, n int, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysListen", syscall, "err", err)
}

func readAddr(addr unsafe.Pointer, addrlen Socklen) (netip.AddrPort, syscall.Errno) {
	rsa := syscallabi.NewValueView((*syscall.RawSockaddr)(addr))

	// XXX: check addrlen here?
	switch rsa.Get().Family {
	case syscall.AF_INET:
		// AF_INET
		if addrlen != syscall.SizeofSockaddrInet4 {
			return netip.AddrPort{}, syscall.EINVAL
		}
		rsa4 := syscallabi.NewValueView((*syscall.RawSockaddrInet4)(addr)).Get()
		ip := netip.AddrFrom4(rsa4.Addr)
		// XXX: HELP
		portBytes := (*[2]byte)(unsafe.Pointer(&rsa4.Port))
		port := uint16(int(portBytes[0])<<8 + int(portBytes[1]))
		for i := 0; i < 8; i++ {
			if rsa4.Zero[i] != 0 {
				return netip.AddrPort{}, syscall.EINVAL
			}
		}
		return netip.AddrPortFrom(ip, port), 0

	default:
		return netip.AddrPort{}, syscall.EINVAL
	}
}

func writeAddr(outbuf syscallabi.ValueView[RawSockaddrAny], outLen syscallabi.ValueView[Socklen], addr netip.AddrPort) syscall.Errno {
	switch {
	case addr.Addr().Is4():
		outbuf := syscallabi.NewValueView((*syscall.RawSockaddrInet4)(outbuf.UnsafePointer()))
		addrPort := addr.Port()
		portBytes := (*[2]byte)(unsafe.Pointer(&addrPort))
		port := uint16(int(portBytes[0])<<8 + int(portBytes[1]))
		outbuf.Set(syscall.RawSockaddrInet4{
			Family: syscall.AF_INET,
			Port:   port,
			Addr:   addr.Addr().As4(),
		})
		outLen.Set(syscall.SizeofSockaddrInet4)
		return 0

	default:
		return syscall.EADDRNOTAVAIL // XXX
	}
}

func (l *LinuxOS) SysAccept4(fd int, rsa syscallabi.ValueView[RawSockaddrAny], len syscallabi.ValueView[Socklen], flags int, invocation *syscallabi.Syscall) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return 0, syscall.EINVAL
	}

	if flags != syscall.SOCK_CLOEXEC|syscall.SOCK_NONBLOCK {
		// XXX: check that flags are CLOEXEC and NONBLOCK (just like sys_socket)
		return 0, syscall.EINVAL // XXX?
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return 0, syscall.EBADFD
	}

	sock, ok := fdInternal.(*Socket)
	if !ok {
		return 0, syscall.ENOTSOCK
	}

	if sock.Listener == nil {
		return 0, syscall.EBADFD
	}

	stream, err := l.machine.netstack.Accept(sock.Listener)
	if err != nil {
		if err == syscall.EWOULDBLOCK {
			return 0, syscall.EWOULDBLOCK
		}
		return 0, syscall.EBADFD
	}

	newFd := l.allocFd()
	l.files[newFd] = &Socket{
		Stream: stream,
	}

	if err := writeAddr(rsa, len, stream.ID().Dest()); err != 0 {
		return 0, err
	}

	return newFd, nil
}

func (customSyscallLogger) LogEntrySysAccept4(s int, rsa *RawSockaddrAny, addrlen *Socklen, flags int, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysAccept4", syscall, "fd", s, "flags", socketFlags.Format(flags))
}

func (customSyscallLogger) LogExitSysAccept4(s int, rsa *RawSockaddrAny, addrlen *Socklen, flags int, syscall *syscallabi.Syscall, fd int, err error) {
	logSyscallExit("SysAccept4", syscall, addrAttr(unsafe.Pointer(rsa), *addrlen), "err", err)
}

func (l *LinuxOS) SysGetsockopt(fd int, level int, name int, ptr unsafe.Pointer, outlen syscallabi.ValueView[Socklen], invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	l.logfFor(invocation, "getsockopt %d %d %d %d", fd, level, name, outlen.Get())

	fdInternal, ok := l.files[fd]
	if !ok {
		return syscall.EBADFD
	}

	sock, ok := fdInternal.(*Socket)
	if !ok {
		return syscall.ENOTSOCK
	}

	switch {
	case sock.Stream != nil:
		if level != syscall.SOL_SOCKET || name != syscall.SO_ERROR {
			return syscall.ENOSYS
		}

		if outlen.Get() != 4 {
			return syscall.EINVAL
		}

		// this is used to determine connection status

		status := l.machine.netstack.StreamStatus(sock.Stream)

		var v uint32
		if !status.Open && !status.Closed {
			v = uint32(syscall.EINPROGRESS)
		} else if !status.Open && status.Closed {
			// XXX: disambiguate later
			v = uint32(syscall.ETIMEDOUT)
		} else {
			// XXX: this is where could also say ECONNREFUSED etc?
			// XXX: supposed to clear error afterwards
			v = 0
		}
		syscallabi.NewValueView[uint32]((*uint32)(ptr)).Set(v)
		return nil

	case sock.Listener != nil:
		// XXX: seems bad response but
		return syscall.ENOSYS

	default:
		return syscall.EBADFD
	}
}

func (l *LinuxOS) SysSetsockopt(s int, level int, name int, val unsafe.Pointer, vallen uintptr, invocation *syscallabi.Syscall) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	// XXX: yep
	return nil
}

func (l *LinuxOS) SysGetsockname(fd int, rsa syscallabi.ValueView[RawSockaddrAny], len syscallabi.ValueView[Socklen], invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return syscall.EBADFD
	}

	sock, ok := fdInternal.(*Socket)
	if !ok {
		return syscall.ENOTSOCK
	}

	switch {
	case sock.Stream != nil:
		if err := writeAddr(rsa, len, sock.Stream.ID().Source()); err != 0 {
			return err
		}
		return nil

	case sock.Listener != nil:
		if err := writeAddr(rsa, len, sock.BoundAddr); err != 0 {
			return err
		}
		return nil

	default:
		return syscall.EBADFD
	}
}

func (customSyscallLogger) LogEntrySysGetsockname(fd int, rsa *RawSockaddrAny, addrlen *Socklen, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysGetsockname", syscall, "fd", fd)
}

func (customSyscallLogger) LogExitSysGetsockname(fd int, rsa *RawSockaddrAny, addrlen *Socklen, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysGetsockname", syscall, addrAttr(unsafe.Pointer(rsa), *addrlen), "err", err)
}

func (l *LinuxOS) SysChdir(path string, invocation *syscallabi.Syscall) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	newDirinode, err := l.machine.filesystem.Getdirinode(l.workdirInode, path)
	if err != nil {
		return err
	}
	l.workdirInode = newDirinode

	return nil
}

func (customSyscallLogger) LogEntrySysChdir(path string, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysChdir", syscall, "path", path)
}

func (customSyscallLogger) LogExitSysChdir(path string, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysChdir", syscall, "err", err)
}

func (l *LinuxOS) SysGetcwd(buf syscallabi.SliceView[byte], invocation *syscallabi.Syscall) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return 0, syscall.EINVAL
	}

	s, err := l.machine.filesystem.Getpath(l.workdirInode)
	if err != nil {
		return 0, err
	}

	if len(s)+1 > buf.Len() {
		// help
		return 0, syscall.EINVAL
	}

	n = buf.Write([]byte(s))
	var zero [1]byte
	n += buf.Slice(n, n+1).Write(zero[:])
	return n, nil
}

func (customSyscallLogger) LogEntrySysGetcwd(buf []byte, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysGetcwd", syscall, "buflen", len(buf))
}

func (customSyscallLogger) LogExitSysGetcwd(buf []byte, syscall *syscallabi.Syscall, n int, err error) {
	logSyscallExit("SysGetcwd", syscall, "buf", string(buf[:n]), "n", n, "err", err)
}

func (l *LinuxOS) SysMkdirat(dirfd int, path string, mode uint32, invocation *syscallabi.Syscall) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	dirInode, err := l.dirInodeForFd(dirfd)
	if err != nil {
		return err
	}

	return l.machine.filesystem.Mkdir(dirInode, path)
}

func (customSyscallLogger) LogEntrySysMkdirat(dirfd int, path string, mode uint32, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysMkdirat", syscall, fdAttr("dirfd", dirfd), "path", path, "mode", fmt.Sprintf("%O", mode))
}

func (customSyscallLogger) LogExitSysMkdirat(dirfd int, path string, mode uint32, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysMkdirat", syscall, "err", err)
}

func (l *LinuxOS) SysGetpeername(fd int, rsa syscallabi.ValueView[RawSockaddrAny], len syscallabi.ValueView[Socklen], invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return syscall.EBADFD
	}

	sock, ok := fdInternal.(*Socket)
	if !ok {
		return syscall.ENOTSOCK
	}

	if sock.Stream == nil {
		return syscall.EBADFD
	}

	if err := writeAddr(rsa, len, sock.Stream.ID().Dest()); err != 0 {
		return err
	}

	return nil
}

func (customSyscallLogger) LogEntrySysGetpeername(fd int, rsa *RawSockaddrAny, addrlen *Socklen, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysGetpeername", syscall, "fd", fd)
}

func (customSyscallLogger) LogExitSysGetpeername(fd int, rsa *RawSockaddrAny, addrlen *Socklen, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysGetpeername", syscall, addrAttr(unsafe.Pointer(rsa), *addrlen), "err", err)
}

// stdioCopy is a copy of a standard file, which fcntl's F_DUPFD makes: it
// writes where the original does.
type stdioCopy struct {
	fd int
}

// stdioFd returns the standard file that fd is or copies, if any.
func (l *LinuxOS) stdioFd(fd int) (int, bool) {
	if c, ok := l.files[fd].(*stdioCopy); ok {
		return c.fd, true
	}
	if _, ok := l.files[fd]; !ok && (fd == syscall.Stdin || fd == syscall.Stdout || fd == syscall.Stderr) {
		return fd, true
	}
	return 0, false
}

// SysFcntl copies the standard files with F_DUPFD and F_DUPFD_CLOEXEC, as
// CockroachDB's logging does to keep the original stderr, and accepts
// F_GETFD and F_SETFD, since a machine runs no other program to inherit
// its files. Other commands fail with ENOSYS.
func (l *LinuxOS) SysFcntl(fd int, cmd int, arg int, invocation *syscallabi.Syscall) (val int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return 0, syscall.EINVAL
	}

	_, isFile := l.files[fd]
	stdio, isStdio := l.stdioFd(fd)
	if !isFile && !isStdio {
		return 0, syscall.EBADF
	}
	switch cmd {
	case syscall.F_DUPFD, syscall.F_DUPFD_CLOEXEC:
		if !isStdio {
			return 0, syscall.ENOSYS
		}
		if arg < 0 || arg >= maxFds {
			return 0, syscall.EINVAL
		}
		newFd, ok := l.lowestFreeFd(arg)
		if !ok {
			return 0, syscall.EMFILE
		}
		l.files[newFd] = &stdioCopy{fd: stdio}
		return newFd, nil
	case syscall.F_GETFD, syscall.F_SETFD:
		return 0, nil
	default:
		return 0, syscall.ENOSYS
	}
}

func (l *LinuxOS) SysFlock(fd int, how int, invocation *syscallabi.Syscall) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// TODO: reconsider
	return nil
}

// SysFcntlFlock takes and tests POSIX record locks. A machine is one process,
// and a process never conflicts with its own locks, while machines do not
// share files. So every lock is granted, and F_GETLK finds no lock that
// would block. The checks are those of Linux: the file must be open, for
// reading to take a read lock and for writing to take a write lock.
func (l *LinuxOS) SysFcntlFlock(fd uintptr, cmd int, lk syscallabi.ValueView[Flock_t], invocation *syscallabi.Syscall) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	fdInternal, ok := l.files[int(fd)]
	if !ok {
		return syscall.EBADF
	}
	f, ok := fdInternal.(*OsFile)
	if !ok {
		return syscall.EINVAL
	}

	switch cmd {
	case syscall.F_GETLK, syscall.F_SETLK, syscall.F_SETLKW:
	default:
		return syscall.EINVAL
	}

	lock := lk.Get()
	switch lock.Type {
	case syscall.F_RDLCK:
		if cmd != syscall.F_GETLK && !f.flagRead {
			return syscall.EBADF
		}
	case syscall.F_WRLCK:
		if cmd != syscall.F_GETLK && !f.flagWrite {
			return syscall.EBADF
		}
	case syscall.F_UNLCK:
		if cmd == syscall.F_GETLK {
			return syscall.EINVAL
		}
	default:
		return syscall.EINVAL
	}

	if cmd == syscall.F_GETLK {
		lock.Type = syscall.F_UNLCK
		lk.Set(lock)
	}
	return nil
}

// The file system that statfs and fstatfs report: ext4's magic number, 4 KiB
// blocks and 1 TiB, all of it free. The simulated disk has no size limit.
const (
	statfsMagic     = 0xef53
	statfsBlockSize = 4096
	statfsBlocks    = 1 << 28
	statfsFiles     = 1 << 26
	statfsNameLen   = 255
)

func fillStatfs(view syscallabi.ValueView[Statfs_t]) {
	view.Set(Statfs_t{
		Type:    statfsMagic,
		Bsize:   statfsBlockSize,
		Blocks:  statfsBlocks,
		Bfree:   statfsBlocks,
		Bavail:  statfsBlocks,
		Files:   statfsFiles,
		Ffree:   statfsFiles,
		Namelen: statfsNameLen,
		Frsize:  statfsBlockSize,
	})
}

func (l *LinuxOS) SysStatfs(path string, buf syscallabi.ValueView[Statfs_t], invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	if _, err := l.machine.filesystem.Stat(l.workdirInode, path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return syscall.ENOENT
		}
		return syscall.EINVAL
	}
	fillStatfs(buf)
	return nil
}

func (l *LinuxOS) SysFstatfs(fd int, buf syscallabi.ValueView[Statfs_t], invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	if _, ok := l.files[fd]; !ok {
		return syscall.EBADF
	}
	fillStatfs(buf)
	return nil
}

// SysFadvise64 accepts advice about a file's access pattern and ignores it:
// the simulated disk has no page cache to read ahead into.
func (l *LinuxOS) SysFadvise64(fd int, offset int64, length int64, advice int, invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return syscall.EBADF
	}
	if _, ok := fdInternal.(*OsFile); !ok {
		return syscall.ESPIPE
	}
	if length < 0 || advice < 0 || advice > 5 {
		return syscall.EINVAL
	}
	return nil
}

func (l *LinuxOS) SysConnect(fd int, addrPtr unsafe.Pointer, addrLen Socklen, invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return syscall.EBADFD
	}

	sock, ok := fdInternal.(*Socket)
	if !ok {
		return syscall.ENOTSOCK
	}

	addr, errno := readAddr(addrPtr, addrLen)
	if errno != 0 {
		return errno
	}

	switch {
	case addr.Addr().Is4():
	default:
		return syscall.EINVAL
	}

	stream, err := l.machine.netstack.OpenStream(addr)
	if err != nil {
		return syscall.EINVAL
	}

	// XXX
	sock.Stream = stream

	// XXX: non blocking now
	return syscall.EINPROGRESS
}

func (customSyscallLogger) LogEntrySysConnect(s int, addr unsafe.Pointer, addrlen Socklen, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysConnect", syscall, addrAttr(addr, addrlen))
}

func (customSyscallLogger) LogExitSysConnect(s int, addr unsafe.Pointer, addrlen Socklen, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysConnect", syscall, "err", err)
}

func (l *LinuxOS) SysGetrandom(ptr syscallabi.ByteSliceView, flags int, invocation *syscallabi.Syscall) (int, error) {
	// TODO: implement entirely in userspace?

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return 0, syscall.EINVAL
	}

	if flags != 0 {
		return 0, syscall.EINVAL
	}

	n := 0
	for ptr.Len() > 0 {
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], gosimruntime.Fastrand64())
		m := ptr.Write(buf[:])
		ptr = ptr.SliceFrom(m)
		n += m
	}
	return n, nil
}

func (customSyscallLogger) LogEntrySysGetrandom(buf []byte, flags int, syscall *syscallabi.Syscall) {
	// logSyscallEntry("SysGetrandom", syscall)
}

func (customSyscallLogger) LogExitSysGetrandom(buf []byte, flags int, syscall *syscallabi.Syscall, n int, err error) {
	// logSyscallExit("SysGetrandom", syscall)
}

func (l *LinuxOS) SysGetpid(invocation *syscallabi.Syscall) int {
	// TODO: return some random number instead?
	return 42
}

func (customSyscallLogger) LogEntrySysGetpid(syscall *syscallabi.Syscall) {
	logSyscallEntry("SysGetpid", syscall)
}

func (customSyscallLogger) LogExitSysGetpid(syscall *syscallabi.Syscall, pid int) {
	logSyscallExit("SysGetpid", syscall, "pid", pid)
}

func (l *LinuxOS) SysUname(buf syscallabi.ValueView[Utsname], invocation *syscallabi.Syscall) (err error) {
	ptr := (*Utsname)(buf.UnsafePointer())
	name := syscallabi.NewSliceView(&ptr.Nodename[0], uintptr(len(ptr.Nodename)))
	var nameArray [65]int8
	n := min(len(l.machine.label), 64)
	for i := 0; i < n; i++ {
		nameArray[i] = int8(l.machine.label[i])
	}
	nameArray[n] = 0
	name.Write(nameArray[:n+1])
	return nil
}

func (customSyscallLogger) LogEntrySysUname(buf *Utsname, syscall *syscallabi.Syscall) {
	logSyscallEntry("SysUname", syscall)
}

func (customSyscallLogger) LogExitSysUname(buf *Utsname, syscall *syscallabi.Syscall, err error) {
	logSyscallExit("SysUname", syscall, slog.Group("buf", int8ArrayToString("nodename", buf.Nodename[:])), "err", err)
}

func (l *LinuxOS) SysMadvise(b syscallabi.SliceView[byte], advice int, invocation *syscallabi.Syscall) (err error) {
	l.logfFor(invocation, "madvise %d %d %d", uintptr(unsafe.Pointer(unsafe.SliceData(b.Ptr))), b.Len(), advice)
	// ignore? check args for some sanity?
	return nil
}

func (l *LinuxOS) SysMmap(addr uintptr, length uintptr, prot int, flags int, fd int, offset int64, invocation *syscallabi.Syscall) (xaddr uintptr, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return 0, syscall.EINVAL
	}

	l.logfFor(invocation, "mmap %d %d %d %d %d %d", addr, length, prot, flags, fd, offset)

	if length > 16*1024*1024 {
		l.logfFor(invocation, "mmap too big")
		return 0, syscall.EINVAL
	}

	if addr != 0 {
		return 0, syscall.EINVAL
	}
	if prot != syscall.PROT_READ {
		return 0, syscall.EINVAL
	}

	flags &= ^syscall.MAP_POPULATE // ignore MAP_POPULATE
	if flags != syscall.MAP_SHARED {
		return 0, syscall.EINVAL
	}
	if offset != 0 {
		return 0, syscall.EINVAL
	}

	fdInternal, ok := l.files[fd]
	if !ok {
		return 0, syscall.EBADFD
	}

	file, ok := fdInternal.(*OsFile)
	if !ok {
		return 0, syscall.EBADFD
	}

	mmap := l.machine.filesystem.Mmap(file.inode, int(length))
	xaddr = uintptr(unsafe.Pointer(unsafe.SliceData(mmap.Data.Ptr)))
	l.mmaps[xaddr] = mmap
	return xaddr, nil
}

func (l *LinuxOS) SysMunmap(addr uintptr, length uintptr, invocation *syscallabi.Syscall) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}

	l.logfFor(invocation, "munmap %d %d", addr, length)

	mmap, ok := l.mmaps[addr]
	if !ok {
		return syscall.EINVAL
	}
	l.machine.filesystem.Munmap(mmap)
	delete(l.mmaps, addr)

	return nil
}

// noMachineDups are the descriptors that noMachineSyscall's F_DUPFD handed
// out in the pass outside any machine and that are not closed. gosim
// translates this package too, so the map is made on first use: the pass
// runs before the package's own initializers.
var (
	noMachineDupsMu sync.Mutex
	noMachineDups   map[int]bool
)

// noMachineSyscall answers a system call made outside any machine. That
// happens when gosimruntime runs every package's initializers once to set
// the globals that machines share: the other globals it computes there are
// thrown away, but a panic is not. Each generated caller passes the name
// of its call when there is no machine.
//
// cockroachdb/ttycolor panics when it cannot stat os.Stdout, so fstat of
// the standard files reports a terminal, as on a machine (SysFstat).
// CockroachDB's logging copies stderr with fcntl, and close frees such a
// copy. Initializers that make keys read random bytes, which come from the
// run's random stream, as on a machine (SysGetrandom). Every other call
// fails with ENOSYS.
//
//go:norace
func noMachineSyscall(name string, s *syscallabi.Syscall) {
	// The goroutine reuses its Syscall: clear what an earlier call left.
	s.R0, s.R1 = 0, 0
	switch name {
	case "SysGetrandom":
		buf := unsafe.Slice(s.Ptr0.(*byte), int(s.Int0))
		for i := 0; i < len(buf); i += 8 {
			var word [8]byte
			binary.LittleEndian.PutUint64(word[:], gosimruntime.Fastrand64())
			copy(buf[i:], word[:])
		}
		s.R0 = uintptr(len(buf))
		s.Errno = 0
		return
	case "SysFcntl":
		fd, cmd, arg := int(s.Int0), int(s.Int1), int(s.Int2)
		if (cmd == syscall.F_DUPFD || cmd == syscall.F_DUPFD_CLOEXEC) && (fd == syscall.Stdin || fd == syscall.Stdout || fd == syscall.Stderr) {
			if arg < 0 || arg >= maxFds {
				s.Errno = uintptr(syscall.EINVAL)
				return
			}
			// The copy is thrown away with the pass, but the pass may compare
			// copies: each gets its own number at or above arg.
			noMachineDupsMu.Lock()
			defer noMachineDupsMu.Unlock()
			if noMachineDups == nil {
				noMachineDups = make(map[int]bool)
			}
			newFd := max(arg, 100)
			for noMachineDups[newFd] {
				newFd++
			}
			if newFd >= maxFds {
				s.Errno = uintptr(syscall.EMFILE)
				return
			}
			noMachineDups[newFd] = true
			s.R0 = uintptr(newFd)
			s.Errno = 0
			return
		}
	case "SysClose":
		// A copy that F_DUPFD made above.
		noMachineDupsMu.Lock()
		defer noMachineDupsMu.Unlock()
		if fd := int(s.Int0); noMachineDups[fd] {
			delete(noMachineDups, fd)
			s.Errno = 0
			return
		}
	case "SysFstat":
		if fd := int(s.Int0); fd == syscall.Stdin || fd == syscall.Stdout || fd == syscall.Stderr {
			stat := s.Ptr1.(*Stat_t)
			*stat = Stat_t{Mode: syscall.S_IFCHR | 0o620, Blksize: 1024}
			s.Errno = 0
			return
		}
	}
	s.Errno = uintptr(syscall.ENOSYS)
}

// SysFchmod accepts any mode for an open file: the simulated file system
// has no permissions, so a mode changes nothing.
func (l *LinuxOS) SysFchmod(fd int, mode uint32, invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}
	if _, ok := l.files[fd]; !ok {
		return syscall.EBADF
	}
	return nil
}

// SysFchmodat accepts any mode for a file that exists, as SysFchmod does.
func (l *LinuxOS) SysFchmodat(dirfd int, path string, mode uint32, invocation *syscallabi.Syscall) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shutdown {
		return syscall.EINVAL
	}
	dirInode, err := l.dirInodeForFd(dirfd)
	if err != nil {
		return err
	}
	if _, err := l.machine.filesystem.Stat(dirInode, path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return syscall.ENOENT
		}
		return syscall.EINVAL
	}
	return nil
}

// isDir reports whether the open inode is a directory, which read fails
// on with EISDIR.
func (l *LinuxOS) isDir(inode int) bool {
	stat, err := l.machine.filesystem.Statfd(inode)
	return err == nil && stat.IsDir
}
