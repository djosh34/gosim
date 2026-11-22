//go:build linux

package go127

import (
	"syscall"
	"unsafe"

	"github.com/jellevandenhooff/gosim/gosimruntime"

	"github.com/jellevandenhooff/gosim/internal/simulation"
)

//go:nocheckptr
//go:uintptrescapes
func GolangOrgXSysUnix_RawSyscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno) {
	return simulation.RawSyscall6(trap, a1, a2, a3, a4, a5, a6)
}

func GolangOrgXSysUnix_SyscallNoError(trap, a1, a2, a3 uintptr) (r1, r2 uintptr) {
	gosimruntime.Unimplemented("golang.org/x/sys/unix.SyscallNoError")
	panic("unreachable")
}

func GolangOrgXSysUnix_RawSyscallNoError(trap, a1, a2, a3 uintptr) (r1, r2 uintptr) {
	r1, r2, _ = simulation.RawSyscall6(trap, a1, a2, a3, 0, 0, 0)
	return r1, r2
}

func GolangOrgXSysUnix_Syscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno) {
	return simulation.RawSyscall6(trap, a1, a2, a3, 0, 0, 0)
}

func GolangOrgXSysUnix_Syscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno) {
	return simulation.RawSyscall6(trap, a1, a2, a3, a4, a5, a6)
}

func GolangOrgXSysUnix_RawSyscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno) {
	return simulation.RawSyscall6(trap, a1, a2, a3, 0, 0, 0)
}

func GolangOrgXSysUnix_gettimeofday(timeval unsafe.Pointer) syscall.Errno {
	gosimruntime.Unimplemented("golang.org/x/sys/unix.gettimeofday")
	panic("unreachable")
}

// GolangOrgXSysUnix_FcntlFlock takes unix.Flock_t, which has the layout of
// syscall.Flock_t.
func GolangOrgXSysUnix_FcntlFlock(fd uintptr, cmd int, lk *syscall.Flock_t) error {
	return simulation.SyscallSysFcntlFlock(fd, cmd, lk)
}

func GolangOrgXSysUnix_fcntl(fd int, cmd, arg int) (int, error) {
	return simulation.SyscallSysFcntl(fd, cmd, arg)
}
