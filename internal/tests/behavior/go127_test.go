//go:build sim

package behavior

import (
	"context"
	_ "embed"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/pprof"
	"slices"
	"sync"
	"testing"
	"weak"

	"github.com/jellevandenhooff/gosim"
)

// Tests for what the Go 1.27 port added or changed.

func TestTempDirAndSetenv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSIM_TEST_VAR", "set")
	if got := os.Getenv("GOSIM_TEST_VAR"); got != "set" {
		t.Errorf("got %q, want set", got)
	}
}

func TestSyncMapRange(t *testing.T) {
	var m sync.Map
	keys := []any{"a", 1, new(int), "b"}
	for i, k := range keys {
		m.Store(k, i)
	}
	var seen []int
	m.Range(func(k, v any) bool {
		seen = append(seen, v.(int))
		return true
	})
	slices.Sort(seen)
	if !slices.Equal(seen, []int{0, 1, 2, 3}) {
		t.Errorf("got %v", seen)
	}
	if v, ok := m.Load(keys[2]); !ok || v != 2 {
		t.Errorf("got %v %v", v, ok)
	}
	if !m.CompareAndSwap("a", 0, 10) {
		t.Error("CompareAndSwap failed")
	}
}

func TestWeakPointer(t *testing.T) {
	p := new(int)
	w := weak.Make(p)
	runtime.GC()
	if w.Value() != p || weak.Make(p) != w {
		t.Error("weak pointer lost its value")
	}
	runtime.AddCleanup(p, func(int) { t.Error("cleanup ran") }, 0)
}

func TestPprofLabels(t *testing.T) {
	ctx := pprof.WithLabels(context.Background(), pprof.Labels("b", "2", "a", "1"))
	ctx = pprof.WithLabels(ctx, pprof.Labels("a", "3"))
	var got []string
	pprof.ForLabels(ctx, func(k, v string) bool {
		got = append(got, k+"="+v)
		return true
	})
	if !slices.Equal(got, []string{"a=3", "b=2"}) {
		t.Errorf("got %v", got)
	}
	pprof.Do(ctx, pprof.Labels("c", "4"), func(ctx context.Context) {
		if v, ok := pprof.Label(ctx, "c"); !ok || v != "4" {
			t.Errorf("got %q %v", v, ok)
		}
	})
}

func TestMapLiteralAddress(t *testing.T) {
	m := &map[string]int{"a": 1}
	(*m)["b"] = 2
	if len(*m) != 2 {
		t.Errorf("got %v", *m)
	}
}

type namedMap map[string]int

func TestMapsOnNamedMap(t *testing.T) {
	m := namedMap{"a": 1}
	c := make(namedMap)
	for k, v := range m {
		c[k] = v
	}
	if !reflect.DeepEqual(m, c) {
		t.Errorf("got %v", c)
	}
	if v, ok := reflect.TypeAssert[namedMap](reflect.ValueOf(m)); !ok || len(v) != 1 {
		t.Errorf("got %v %v", v, ok)
	}
}

//go:embed testdata/embed.txt
var embedded string

func TestEmbed(t *testing.T) {
	if embedded != "embedded by TestEmbed\n" {
		t.Errorf("got %q", embedded)
	}
}

func TestTestFuncAsValue(t *testing.T) {
	t.Run("embed", TestEmbed)
}

func TestWriteArtifact(t *testing.T) {
	gosim.WriteArtifact("artifact.txt", []byte("hello"))
}

func TestUnimplementedHook(t *testing.T) {
	if os.Getenv("TESTINGFAIL") != "1" {
		t.Skip()
	}
	defer func() {
		recover()
		t.Error("recovered from an unimplemented hook")
	}()
	reflect.MapOf(reflect.TypeFor[string](), reflect.TypeFor[int]())
}

func TestFatalUnlock(t *testing.T) {
	if os.Getenv("TESTINGFAIL") != "1" {
		t.Skip()
	}
	defer func() {
		recover()
		t.Error("recovered from a fatal error")
	}()
	var mu sync.Mutex
	mu.Unlock()
}

func TestOpenRoot(t *testing.T) {
	f, err := os.Open("/")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Error(err)
	}
	if err := f.Close(); err != nil {
		t.Error(err)
	}
	if info, err := os.Stat("/"); err != nil || !info.IsDir() {
		t.Errorf("got %v %v", info, err)
	}
}

func TestLoopbackListener(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		c, err := l.Accept()
		if err != nil {
			t.Error(err)
			return
		}
		c.Write([]byte("hi"))
		c.Close()
	}()
	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(c)
	if err != nil || string(b) != "hi" {
		t.Errorf("got %q %v", b, err)
	}
}
