package gosimruntime

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime"
	"sync"
)

// A simulation can keep bytes in files on the disk of the machine that
// runs it, outside the simulation, to hold more than memory would: a host
// file. Its reads and writes go to the real disk at once, never block the
// simulation's clock and draw nothing from the run's random stream, so
// they change nothing in the run. Each host file is removed from the
// directory when it is made, so the disk gets its space back when the
// file is closed, at the latest when its run ends.

// hostMu guards the host files. One run at a time uses them, one goroutine
// at a time, but the end of a run closes them from the scheduler's side.
var (
	hostMu    sync.Mutex
	hostFiles = map[int]*os.File{}
	hostSlots = map[int]*slots{}
	hostNext  int
)

// slots are the slots of one host file: of size bytes each, end bytes in
// all, the free ones in free.
type slots struct {
	size int64
	end  int64
	free []int64
}

// ErrNoHostFile is the error of a call on a host file that is closed or
// was never made. The simulation sees it as its own fs.ErrClosed.
var ErrNoHostFile = errors.New("gosim: no such host file")

// HostCreate makes an empty host file in the directory of
// os.CreateTemp, and returns its handle.
func HostCreate() (int, error) {
	f, err := os.CreateTemp("", "gosim-host-*")
	if err != nil {
		return 0, err
	}
	if err := os.Remove(f.Name()); err != nil {
		return 0, errors.Join(err, f.Close())
	}
	hostMu.Lock()
	defer hostMu.Unlock()
	hostNext++
	hostFiles[hostNext] = f
	return hostNext, nil
}

func hostFile(h int) (*os.File, error) {
	hostMu.Lock()
	defer hostMu.Unlock()
	f, ok := hostFiles[h]
	if !ok {
		return nil, ErrNoHostFile
	}
	return f, nil
}

// HostWriteAt writes p at offset off of host file h.
func HostWriteAt(h int, p []byte, off int64) (int, error) {
	f, err := hostFile(h)
	if err != nil {
		return 0, err
	}
	return f.WriteAt(p, off)
}

// HostReadAt reads len(p) bytes at offset off of host file h, as
// io.ReaderAt does, and reports with eof that it reached the end: the
// simulation's io.EOF is not this package's.
func HostReadAt(h int, p []byte, off int64) (n int, eof bool, err error) {
	f, err := hostFile(h)
	if err != nil {
		return 0, false, err
	}
	n, err = f.ReadAt(p, off)
	if errors.Is(err, io.EOF) {
		return n, true, nil
	}
	return n, false, err
}

// closeHostFiles closes the host files a run left open, as it ends.
func closeHostFiles() error {
	hostMu.Lock()
	defer hostMu.Unlock()
	var errs []error
	for h, f := range hostFiles {
		errs = append(errs, f.Close())
		delete(hostFiles, h)
		delete(hostSlots, h)
	}
	return errors.Join(errs...)
}

// ErrSlotSize is the error of a slot of a size other than the host
// file's first slot's.
var ErrSlotSize = errors.New("gosim: a host file's slots all have one size")

// ErrSlotOwner is the error of a slot whose owner is not a pointer.
var ErrSlotOwner = errors.New("gosim: a host file slot's owner must be a pointer")

// HostSlot returns the offset of a free slot of size bytes in host file
// h, which stays taken while owner is reachable. Once the collector finds
// owner unreachable, a finalizer gives the slot back for a later call.
// owner must point to the start of an object of its own, with no other
// finalizer, as runtime.SetFinalizer requires.
//
// Which slot a call gets depends on when the collector ran, so it differs
// from run to run: a run must use the offset only to read and write the
// file.
func HostSlot(h int, owner any, size int) (int64, error) {
	if v := reflect.ValueOf(owner); v.Kind() != reflect.Pointer || v.IsNil() {
		return 0, fmt.Errorf("%w: %T", ErrSlotOwner, owner)
	}
	hostMu.Lock()
	defer hostMu.Unlock()
	if _, ok := hostFiles[h]; !ok {
		return 0, ErrNoHostFile
	}
	s := hostSlots[h]
	if s == nil {
		s = &slots{size: int64(size)}
		hostSlots[h] = s
	}
	if s.size != int64(size) {
		return 0, fmt.Errorf("%w: %d bytes, not %d", ErrSlotSize, size, s.size)
	}
	var off int64
	if n := len(s.free); n > 0 {
		off, s.free = s.free[n-1], s.free[:n-1]
	} else {
		off, s.end = s.end, s.end+s.size
	}
	// The finalizer runs on the collector's goroutine, outside the
	// simulation; it only gives the slot back, under hostMu.
	runtime.SetFinalizer(owner, func(any) { freeSlot(h, s, off) })
	return off, nil
}

// freeSlot gives slot off of host file h back, if the file is still open.
func freeSlot(h int, s *slots, off int64) {
	hostMu.Lock()
	defer hostMu.Unlock()
	if hostSlots[h] == s {
		s.free = append(s.free, off)
	}
}

// HostClose closes host file h, which gives its space back.
func HostClose(h int) error {
	hostMu.Lock()
	f, ok := hostFiles[h]
	delete(hostFiles, h)
	delete(hostSlots, h)
	hostMu.Unlock()
	if !ok {
		return ErrNoHostFile
	}
	return f.Close()
}
