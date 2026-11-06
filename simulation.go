package gosim

import (
	"errors"
	"io"
	"io/fs"
	"time"

	"github.com/jellevandenhooff/gosim/gosimruntime"
	"github.com/jellevandenhooff/gosim/internal/simulation"
)

// IsSim returns if the program is running inside of a gosim simulation.
//
// To include or exclude code or tests only if running inside of a gosim the
// gosim tool also supports a build tag. Use the `//go:build sim` build
// constraint at the top of a go file to only include it in simulated builds or
// `//go:build !sim` to exclude it from simulated from builds.
func IsSim() bool {
	return gosimruntime.IsSim()
}

// Yield yields the processor, allowing other goroutines to run. Similar to
// [runtime.Gosched] but for gosim's scheduler.
//
// TODO: Replace with runtime.Gosched() instead?
func Yield() {
	gosimruntime.Yield()
}

/*
type Simulation struct{}

func CurrentSimulation() Simulation {
	return Simulation{}
}

// SetConnected configures the network connectivity between addresses a and b.
//
// TODO: How will this fail? Unrouteable or rejected
// TODO: Support asymmetry
func (s Simulation) SetConnected(a, b string, connected bool) error {
	return simulation.SyscallSetConnected(a, b, connected)
}

// SetDelay configures the network latency between addresses a and b.
//
// TODO: Support asymmetry
func (s Simulation) SetDelay(a, b string, delay time.Duration) error {
	return simulation.SyscallSetDelay(a, b, delay)
}

// SetSimulationTimeout resets the simulation timeout to the given duration.
// After the simulated duration the simululation will fail with a test timeout
// error.
func (s Simulation) SetSimulationTimeout(timeout time.Duration) {
	err := simulation.SyscallSetSimulationTimeout(timeout)
	if err != nil {
		panic(err)
	}
}
*/

// SetConnected configures the network connectivity between addresses a and b.
//
// TODO: How will this fail? Unrouteable or rejected
// TODO: Support asymmetry
func SetConnected(a, b string, connected bool) error {
	return simulation.SyscallSetConnected(a, b, connected)
}

// SetDelay configures the network latency between addresses a and b.
//
// TODO: Support asymmetry
func SetDelay(a, b string, delay time.Duration) error {
	return simulation.SyscallSetDelay(a, b, delay)
}

// SetSimulationTimeout resets the simulation timeout to the given duration.
// After the simulated duration the simululation will fail with a test timeout
// error.
func SetSimulationTimeout(d time.Duration) {
	err := simulation.SyscallSetSimulationTimeout(d)
	if err != nil {
		panic(err)
	}
}

// WallLimitPassed reports whether the current run's limit of real time,
// set by the test that started it with metatesting.RunConfig.WallLimit, has
// passed. A simulation reads it where it can stop and report itself; until
// the limit passes it changes nothing in the run.
func WallLimitPassed() bool {
	return gosimruntime.WallLimitPassed()
}

// CPUTime returns the CPU time the process has used so far, user and
// system, on all of its threads: the simulation's and the garbage
// collector's. A simulation reads it to measure what its work costs.
// Reading it changes nothing in the run. Its value differs from run to
// run, so the run must not act on it or log it.
func CPUTime() (time.Duration, error) {
	ns, err := gosimruntime.CPUTime()
	return time.Duration(ns), err
}

// A HostFile is a file on the disk of the machine that runs the
// simulation, outside it, for a simulation that keeps more bytes than
// memory holds. Its reads and writes go to the real disk at once and
// change nothing in the run, unless the host's disk fails them. It is
// closed when its run ends, if not before.
//
// A call on a closed HostFile fails with fs.ErrClosed, and a read at the
// end with io.EOF. Other errors are the host's, which errors.Is cannot
// match against the simulation's own os, io/fs and syscall errors: they
// are for reports.
type HostFile struct{ h int }

// CreateHostFile makes an empty HostFile.
func CreateHostFile() (*HostFile, error) {
	h, err := gosimruntime.HostCreate()
	if err != nil {
		return nil, err
	}
	return &HostFile{h: h}, nil
}

// hostErr is err as the simulation sees it.
func hostErr(err error) error {
	if errors.Is(err, gosimruntime.ErrNoHostFile) {
		return fs.ErrClosed
	}
	return err
}

// WriteAt writes p at offset off.
func (f *HostFile) WriteAt(p []byte, off int64) (int, error) {
	n, err := gosimruntime.HostWriteAt(f.h, p, off)
	return n, hostErr(err)
}

// ReadAt reads len(p) bytes at offset off, as io.ReaderAt does.
func (f *HostFile) ReadAt(p []byte, off int64) (int, error) {
	n, eof, err := gosimruntime.HostReadAt(f.h, p, off)
	if eof {
		return n, io.EOF
	}
	return n, hostErr(err)
}

// Slot returns the offset of a free slot of size bytes in the file, for
// the caller to write and read, which stays taken while owner is
// reachable: once owner is garbage, a later call may get the slot again.
// owner must be a pointer to at least a byte, which may point into an
// object and may own several slots; an object of less than 16 bytes
// without pointers may share its memory with others and keep its slots
// taken for good. Every slot of a file has the size of its first.
//
// Which slot a call gets depends on when the collector ran, so it differs
// from run to run, and so does what a slot holds before it is written. So
// that nothing in the run changes, the run must use the offset only to
// read and write the slot, read only bytes it wrote there since Slot
// returned it, and keep owner reachable until its last read or write of
// the slot (runtime.KeepAlive).
func (f *HostFile) Slot(owner any, size int) (int64, error) {
	off, err := gosimruntime.HostSlot(f.h, owner, size)
	return off, hostErr(err)
}

// Close closes the file and gives its space back.
func (f *HostFile) Close() error {
	return hostErr(gosimruntime.HostClose(f.h))
}

// Stacks returns the stack of every goroutine of the process, the
// simulation's included, for the report of a run that went on too long.
func Stacks() []byte {
	return gosimruntime.Stacks()
}

// WriteArtifact saves a copy of data under name in the result of the current
// run, for the test that started the run: a test using metatesting reads it
// from RunResult.Artifacts. The simulation cannot read it back. Writing a name
// again replaces its data.
func WriteArtifact(name string, data []byte) {
	gosimruntime.WriteArtifact(name, data)
}
