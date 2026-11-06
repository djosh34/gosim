package gosimruntime

import (
	"errors"
	"io"
	"os"
	"sync"
)

// A simulation can keep bytes in files on the disk of the machine that
// runs it, outside the simulation, to hold more than memory would: a host
// file. Its reads and writes go to the real disk at once, never block the
// simulation's clock and draw nothing from the run's random stream, so
// they change nothing in the run. Each host file is removed from the
// directory when it is made, so the disk gets its space back when the
// file is closed, at the latest when its run ends.

var (
	hostMu    sync.Mutex
	hostFiles = map[int]*os.File{}
	hostNext  int
)

// ErrNoHostFile is the error of a call on a host file that is closed or
// was never made.
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
	}
	return errors.Join(errs...)
}

// HostClose closes host file h, which gives its space back.
func HostClose(h int) error {
	hostMu.Lock()
	f, ok := hostFiles[h]
	delete(hostFiles, h)
	hostMu.Unlock()
	if !ok {
		return ErrNoHostFile
	}
	return f.Close()
}
