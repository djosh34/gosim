//go:build !sim

package metatesting

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jellevandenhooff/gosim/gosimruntime"
)

var errTestDone = errors.New("the test is done")

// fakeBinary makes this test binary, started with $GOSIM_FAKE_METATEST
// set, a metatest binary that answers each run as a run with a data race
// does: the report on stderr, then the result.
const fakeBinary = "GOSIM_FAKE_METATEST"

func TestMain(m *testing.M) {
	if os.Getenv(fakeBinary) != "" {
		in := json.NewDecoder(os.Stdin)
		out := json.NewEncoder(os.Stdout)
		for {
			var cfg RunConfig
			if err := in.Decode(&cfg); err != nil {
				os.Exit(0)
			}
			fmt.Fprintln(os.Stderr, "WARNING: DATA RACE")
			// A run can leave its last line without a newline.
			fmt.Fprint(os.Stderr, "no newline")
			fmt.Fprintln(os.Stderr, gosimruntime.MetatestResultMark)
			if err := out.Encode(RunResult{Seed: cfg.Seed, Failed: true, Err: "race detected"}); err != nil {
				os.Exit(1)
			}
		}
	}
	os.Exit(m.Run())
}

// slowWriter keeps what it is given, each write after a delay.
type slowWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *slowWriter) Write(p []byte) (int, error) {
	time.Sleep(200 * time.Millisecond)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *slowWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// TestStderrBeforeResult checks that a run's result comes back only after
// what the run wrote to stderr before it was forwarded, however slowly
// (tigerstore #530).
func TestStderrBeforeResult(t *testing.T) {
	t.Setenv(fakeBinary, "1")
	w := &slowWriter{}
	forward = w
	defer func() { forward = os.Stderr }()
	r, err := newRunner(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	// fail ends the binary, as after a run that broke it; its error says
	// that it was killed.
	defer func() { _ = r.fail(errTestDone) }()
	res, err := r.RunQuiet(&RunConfig{Test: "TestBad", Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Error("the run passed")
	}
	if got := w.String(); !strings.Contains(got, "metatest binary: WARNING: DATA RACE\nmetatest binary: no newline\n") {
		t.Errorf("the result came back before the run's stderr: forwarded %q", got)
	}
	if strings.Contains(w.String(), gosimruntime.MetatestResultMark) {
		t.Error("the result mark was forwarded")
	}
}
