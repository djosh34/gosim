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

// TimerFirings returns how many times a timer fired in the current run so
// far, on every machine: the end of a time.Sleep, each time a timer or
// ticker fires, also when its tick is dropped because nobody read the one
// before, and each AfterFunc. It counts the simulator's own timers too,
// such as the 15 s keepalive on each side of an idle simulated TCP
// connection, so an idle run does not count zero. A simulation reads it to
// count how often its code wakes up on time. Reading it changes nothing in
// the run, and the same run counts the same.
func TimerFirings() uint64 {
	return gosimruntime.TimerFirings()
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

// OpenInput opens the run's input name, a file on the host that the test
// that started the run gave it (metatesting.RunConfig.Inputs), as a
// HostFile to read. Opening it, its size and its reads change nothing in
// the run, as a HostFile's reads do: none takes a draw or yields, in any
// build, nor does a failed open. An input the run was not given fails with
// a *fs.PathError whose Err is fs.ErrNotExist. One it was given whose file
// the host cannot open fails with the host's error, which errors.Is
// cannot match against the simulation's own: the test that named the
// file got it wrong. A write to an input fails with fs.ErrPermission.
func OpenInput(name string) (*HostFile, error) {
	h, err := gosimruntime.HostOpenInput(name)
	if errors.Is(err, gosimruntime.ErrNoInput) {
		// Built without fmt: a translated fmt takes a draw under the race
		// detector, as it returns its printer to a sync.Pool.
		return nil, &fs.PathError{Op: "openinput", Path: name, Err: fs.ErrNotExist}
	}
	if err != nil {
		return nil, err
	}
	return &HostFile{h: h}, nil
}

// CreateOutput makes the run's output name, a file on the host that the
// test that started the run named (metatesting.RunConfig.Outputs), or cuts
// it to empty, as a HostFile to write and read. The file stays on the host
// after the run, for the test to read. Making it and writing it change
// nothing in the run, as a HostFile's writes do: none takes a draw or
// yields, in any build, nor does a failed make. An output the run was not
// given fails with a *fs.PathError whose Err is fs.ErrNotExist. One it was
// given whose file the host cannot make fails with the host's error.
func CreateOutput(name string) (*HostFile, error) {
	h, err := gosimruntime.HostCreateOutput(name)
	if errors.Is(err, gosimruntime.ErrNoOutput) {
		// Built without fmt, as OpenInput's error.
		return nil, &fs.PathError{Op: "createoutput", Path: name, Err: fs.ErrNotExist}
	}
	if err != nil {
		return nil, err
	}
	return &HostFile{h: h}, nil
}

// Size returns the file's size in bytes.
func (f *HostFile) Size() (int64, error) {
	n, err := gosimruntime.HostSize(f.h)
	return n, hostErr(err)
}

// hostErr is err as the simulation sees it.
func hostErr(err error) error {
	switch {
	case errors.Is(err, gosimruntime.ErrNoHostFile):
		return fs.ErrClosed
	case errors.Is(err, gosimruntime.ErrInputWrite):
		return fs.ErrPermission
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

// Close closes the file. A file of NewHostFile gives its space back; an
// input or an output stays on the host.
func (f *HostFile) Close() error {
	return hostErr(gosimruntime.HostClose(f.h))
}

// Stacks returns the stack of every goroutine of the process, the
// simulation's included, for the report of a run that went on too long.
func Stacks() []byte {
	return gosimruntime.Stacks()
}

// Reseed sets the run's random stream from seed, as a run that starts with
// seed sets it: the scheduler's picks and every random value drawn after
// it, by math/rand, math/rand/v2, crypto/rand, select and new maps, come
// from seed. A simulation calls it to start a part of a run that must
// replay alone, such as each of many scenarios that start from one
// shared state. The part replays alone when it starts in the same state
// each time: at the same simulated time, with no other goroutine
// runnable and no timer pending that it can observe, and with nothing it
// reads made before Reseed, such as a map's order, a hash seed or bytes a
// math/rand source buffered. Timers due at the same time fire in the
// order they were set, whatever the heap held before. Goroutine IDs keep
// counting, so logs and the run's checksum still differ.
func Reseed(seed int64) {
	gosimruntime.Reseed(seed)
}

// WriteArtifact saves a copy of data under name in the result of the current
// run, for the test that started the run: a test using metatesting reads it
// from RunResult.Artifacts. The simulation cannot read it back. Writing a name
// again replaces its data.
func WriteArtifact(name string, data []byte) {
	gosimruntime.WriteArtifact(name, data)
}
