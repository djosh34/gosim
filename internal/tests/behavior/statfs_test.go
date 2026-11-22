package behavior_test

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// Pebble reads the free space of its directory with statfs, and the file
// system type of its files with fstatfs.

func TestDiskStatfs(t *testing.T) {
	setupRealDisk(t)

	var stat unix.Statfs_t
	if err := unix.Statfs(".", &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Bsize <= 0 || stat.Blocks == 0 || stat.Bavail > stat.Blocks {
		t.Errorf("got block size %d, %d blocks, %d available", stat.Bsize, stat.Blocks, stat.Bavail)
	}

	if err := unix.Statfs("missing", &stat); !errors.Is(err, unix.ENOENT) {
		t.Errorf("statfs of a missing file: got %v, want ENOENT", err)
	}
}

func TestDiskFstatfs(t *testing.T) {
	setupRealDisk(t)

	f, err := os.Create("file")
	if err != nil {
		t.Fatal(err)
	}
	fd := int(f.Fd())
	var stat unix.Statfs_t
	if err := unix.Fstatfs(fd, &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Bsize <= 0 || stat.Blocks == 0 {
		t.Errorf("got block size %d, %d blocks", stat.Bsize, stat.Blocks)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Fstatfs(fd, &stat); !errors.Is(err, unix.EBADF) {
		t.Errorf("fstatfs of a closed file: got %v, want EBADF", err)
	}
}

func TestDiskFadvise(t *testing.T) {
	setupRealDisk(t)

	f, err := os.Create("file")
	if err != nil {
		t.Fatal(err)
	}
	fd := int(f.Fd())
	if err := unix.Fadvise(fd, 0, 0, unix.FADV_SEQUENTIAL); err != nil {
		t.Error(err)
	}
	if err := unix.Fadvise(fd, 0, 0, unix.FADV_RANDOM); err != nil {
		t.Error(err)
	}
	if err := unix.Fadvise(fd, 0, 0, 100); !errors.Is(err, unix.EINVAL) {
		t.Errorf("unknown advice: got %v, want EINVAL", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Fadvise(fd, 0, 0, unix.FADV_RANDOM); !errors.Is(err, unix.EBADF) {
		t.Errorf("fadvise of a closed file: got %v, want EBADF", err)
	}
}

// Pebble locks its directory with a write lock on a file, as here.
func TestDiskFcntlFlock(t *testing.T) {
	setupRealDisk(t)

	f, err := os.Create("LOCK")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lock := unix.Flock_t{Type: unix.F_WRLCK}
	if err := unix.FcntlFlock(f.Fd(), unix.F_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	// A process does not conflict with its own locks.
	if err := unix.FcntlFlock(f.Fd(), unix.F_SETLK, &lock); err != nil {
		t.Error(err)
	}
	probe := unix.Flock_t{Type: unix.F_WRLCK}
	if err := unix.FcntlFlock(f.Fd(), unix.F_GETLK, &probe); err != nil {
		t.Fatal(err)
	}
	if probe.Type != unix.F_UNLCK {
		t.Errorf("F_GETLK by the lock's owner: got type %d, want F_UNLCK", probe.Type)
	}
	unlock := unix.Flock_t{Type: unix.F_UNLCK}
	if err := unix.FcntlFlock(f.Fd(), unix.F_SETLK, &unlock); err != nil {
		t.Error(err)
	}

	r, err := os.Open("LOCK")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := unix.FcntlFlock(r.Fd(), unix.F_SETLK, &lock); !errors.Is(err, unix.EBADF) {
		t.Errorf("write lock on a file open for reading: got %v, want EBADF", err)
	}
	if err := unix.FcntlFlock(r.Fd(), unix.F_SETLK, &unix.Flock_t{Type: unix.F_RDLCK}); err != nil {
		t.Error(err)
	}
}
