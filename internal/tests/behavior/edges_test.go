package behavior_test

import (
	"embed"
	"errors"
	"io/fs"
	"math/big"
	"math/rand"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jellevandenhooff/gosim"
)

// Constructs that translation once got wrong.

// varNilMap is a blank global that is nil.
var _ map[string]int = nil

type hidden map[string]int

type holder struct{ h hidden }

// hide's parameter hides the type hidden, which the translated assignment
// of a map to h names.
func hide(hidden []string) holder {
	m := make(map[string]int, 0)
	for _, k := range hidden {
		m[k]++
	}
	return holder{h: m}
}

func TestShadowedMapType(t *testing.T) {
	if got := hide([]string{"a", "a", "b"}).h; got["a"] != 2 || got["b"] != 1 {
		t.Errorf("got %v", got)
	}
}

func TestMakeSizes(t *testing.T) {
	n := uint16(3)
	ch := make(chan int, n)
	ch <- 1
	ch <- 2
	ch <- 3
	if len(ch) != 3 || cap(ch) != 3 {
		t.Errorf("len %d, cap %d", len(ch), cap(ch))
	}
	size := 10
	m := make(map[int]int, size)
	m[1] = 1
	if len(m) != 1 {
		t.Errorf("got %v", m)
	}
}

func TestBigIntRand(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	n := big.NewInt(1000)
	seen := make(map[int64]bool)
	for range 200 {
		v := new(big.Int).Rand(r, n)
		if v.Sign() < 0 || v.Cmp(n) >= 0 {
			t.Fatalf("got %v, not in [0, 1000)", v)
		}
		seen[v.Int64()] = true
	}
	if len(seen) < 100 {
		t.Errorf("only %d values in 200 draws", len(seen))
	}
	if v := new(big.Int).Rand(r, big.NewInt(0)); v.Sign() != 0 {
		t.Errorf("Rand with n = 0 got %v", v)
	}
}

func TestGoPrintln(t *testing.T) {
	done := make(chan struct{})
	go println("from a goroutine")
	go func() { close(done) }()
	<-done
}

var builders map[string]*strings.Builder

// setBuilders's parameter hides the package strings in the body, where the
// translated nil names *strings.Builder.
func setBuilders(strings map[string]*strings.Builder) func() {
	builders = strings
	return func() { builders = nil }
}

func TestShadowedPackage(t *testing.T) {
	reset := setBuilders(map[string]*strings.Builder{"a": {}})
	if len(builders) != 1 {
		t.Errorf("got %v", builders)
	}
	reset()
	if builders != nil {
		t.Errorf("got %v after the reset", builders)
	}
}

// chanSet is a set of channels, as in a struct field of CockroachDB.
type chanSet struct {
	mu struct {
		chans map[chan<- struct{}]struct{}
	}
}

func TestMapOfChannels(t *testing.T) {
	var s chanSet
	s.mu.chans = make(map[chan<- struct{}]struct{})
	ch := make(chan struct{}, 1)
	s.mu.chans[ch] = struct{}{}
	for c := range s.mu.chans {
		select {
		case c <- struct{}{}:
		default:
			t.Error("the channel is full")
		}
	}
	delete(s.mu.chans, ch)
	if len(s.mu.chans) != 0 || len(ch) != 1 {
		t.Errorf("got %d channels, %d in the channel", len(s.mu.chans), len(ch))
	}
}

type optMap map[string]int

func takesOpt(m optMap) int { return len(m) }

var globalOpts = map[string]int{"a": 1, "b": 2}

func TestGlobalMapConversion(t *testing.T) {
	if n := takesOpt(globalOpts); n != 2 {
		t.Errorf("got %d", n)
	}
}

//go:embed testdata/embed.txt
var embedded embed.FS

// TestEmbedFSAsFS passes an embed.FS where an fs.FS is expected.
func TestEmbedFSAsFS(t *testing.T) {
	data, err := fs.ReadFile(embedded, "testdata/embed.txt")
	if err != nil || len(data) == 0 {
		t.Fatalf("got %q, %v", data, err)
	}
	var names []string
	if err := fs.WalkDir(embedded, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		names = append(names, p)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{".", "testdata", "testdata/embed.txt"}; !slices.Equal(names, want) {
		t.Errorf("got %q, want %q", names, want)
	}
	info, err := fs.Stat(embedded, "testdata/embed.txt")
	if err != nil || info.Size() != int64(len(data)) || info.IsDir() {
		t.Errorf("got %v, %v", info, err)
	}
	if _, err := fs.Stat(embedded, "missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a missing file: got %v", err)
	}
}

// TestStatStdio stats the standard files, as cockroachdb/ttycolor does
// when it starts. In a simulation they are character devices.
func TestStatStdio(t *testing.T) {
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		info, err := f.Stat()
		if err != nil {
			t.Fatalf("%s: %v", f.Name(), err)
		}
		// Natively, go test's output may be a pipe.
		if gosim.IsSim() && info.Mode()&os.ModeCharDevice == 0 {
			t.Errorf("%s: mode %v, not a character device", f.Name(), info.Mode())
		}
	}
}
