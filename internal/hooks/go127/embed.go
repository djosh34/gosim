package go127

import (
	"embed"
	"io"
	"io/fs"
	"strings"
	"time"
)

// EmbedFS gives translated code the files of an embed.FS as an fs.FS, for
// an embed.FS passed where an fs.FS is expected (rewriteEmbedFS in
// cmd/gosim). gosim does not translate embed, whose FS implements the
// native io/fs interfaces, not the translated ones. The files keep their
// names, contents and modes; their times are zero, as in embed. It returns
// the wrapper itself, not an fs.FS, so that it converts to fs.ReadFileFS
// and fs.ReadDirFS too.
func EmbedFS(e embed.FS) embedFS { return embedFS{e: e} }

type embedFS struct{ e embed.FS }

// embedErr turns embed's error into a translated *fs.PathError. embed
// reports a missing file with the native fs.ErrNotExist, which translated
// code cannot name; its message tells it apart.
func embedErr(op, name string, err error) error {
	if strings.HasSuffix(err.Error(), fs.ErrNotExist.Error()) {
		err = fs.ErrNotExist
	}
	return &fs.PathError{Op: op, Path: name, Err: err}
}

func (f embedFS) Open(name string) (fs.File, error) {
	file, err := f.e.Open(name)
	if err != nil {
		return nil, embedErr("open", name, err)
	}
	native, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, embedErr("stat", name, err)
	}
	info := embedInfo{name: native.Name(), size: native.Size(), mode: fs.FileMode(native.Mode())}
	return &embedFile{file: file, info: info, fs: f, name: name}, nil
}

func (f embedFS) ReadFile(name string) ([]byte, error) {
	data, err := f.e.ReadFile(name)
	if err != nil {
		return nil, embedErr("open", name, err)
	}
	return data, nil
}

func (f embedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := f.e.ReadDir(name)
	if err != nil {
		return nil, embedErr("open", name, err)
	}
	out := make([]fs.DirEntry, 0, len(entries))
	for _, d := range entries {
		native, err := d.Info()
		if err != nil {
			return nil, embedErr("stat", name, err)
		}
		out = append(out, embedInfo{name: native.Name(), size: native.Size(), mode: fs.FileMode(native.Mode())})
	}
	return out, nil
}

// embedFile is an open file of an embedFS.
type embedFile struct {
	file interface {
		Read([]byte) (int, error)
		Close() error
	}
	info embedInfo
	fs   embedFS
	name string
	// dir holds the entries that ReadDir has yet to return, once it read
	// the directory.
	dir     []fs.DirEntry
	dirRead bool
}

func (f *embedFile) Read(p []byte) (int, error) { return f.file.Read(p) }
func (f *embedFile) Close() error               { return f.file.Close() }
func (f *embedFile) Stat() (fs.FileInfo, error) { return f.info, nil }

// ReadDir returns the directory's next n entries, as fs.ReadDirFile says:
// with n > 0, at most n of them, and io.EOF once none are left; with n <= 0,
// all that are left.
func (f *embedFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if !f.info.IsDir() {
		return nil, &fs.PathError{Op: "read", Path: f.name, Err: fs.ErrInvalid}
	}
	if !f.dirRead {
		entries, err := f.fs.ReadDir(f.name)
		if err != nil {
			return nil, err
		}
		f.dir, f.dirRead = entries, true
	}
	if n <= 0 {
		entries := f.dir
		f.dir = nil
		return entries, nil
	}
	if len(f.dir) == 0 {
		return nil, io.EOF
	}
	n = min(n, len(f.dir))
	entries := f.dir[:n:n]
	f.dir = f.dir[n:]
	return entries, nil
}

// embedInfo is both the fs.FileInfo and the fs.DirEntry of a file.
type embedInfo struct {
	name string
	size int64
	mode fs.FileMode
}

func (i embedInfo) Name() string               { return i.name }
func (i embedInfo) Size() int64                { return i.size }
func (i embedInfo) Mode() fs.FileMode          { return i.mode }
func (i embedInfo) ModTime() time.Time         { return time.Time{} }
func (i embedInfo) IsDir() bool                { return i.mode.IsDir() }
func (i embedInfo) Sys() any                   { return nil }
func (i embedInfo) Type() fs.FileMode          { return i.mode.Type() }
func (i embedInfo) Info() (fs.FileInfo, error) { return i, nil }
