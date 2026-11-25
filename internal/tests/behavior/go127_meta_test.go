//go:build !sim

package behavior_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jellevandenhooff/gosim/metatesting"
)

func TestWriteArtifact(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestWriteArtifact", Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if run.Failed || string(run.Artifacts["artifact.txt"]) != "hello" {
		t.Errorf("got failed %v, artifacts %q", run.Failed, run.Artifacts)
	}
}

// TestOutputChangesNothing runs TestOutput with no output, with an output
// that is there already (which it cuts), with a new one and one it leaves
// open, and with an output it does not make: all draw the same values and
// end with the same checksum, and the host's files hold what the runs
// wrote. TestOutputSkipped, which never calls CreateOutput, draws the
// same values too, so no call took a draw, not even the first. An output
// whose file the host cannot make fails with the host's error.
func TestOutputChangesNothing(t *testing.T) {
	dir := t.TempDir()
	there, fresh, open, other := filepath.Join(dir, "there"), filepath.Join(dir, "fresh"), filepath.Join(dir, "open"), filepath.Join(dir, "other")
	// A file there already is cut, not written over in part.
	if err := os.WriteFile(there, []byte("an older and longer content than the run's"), 0o600); err != nil {
		t.Fatal(err)
	}
	mt := metatesting.ForCurrentPackage(t)
	var runs []*metatesting.RunResult
	outs := []map[string]string{nil, {"out": there}, {"out": fresh, "open": open}, {"unused": other}}
	for _, out := range outs {
		run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestOutput", Seed: 3, Outputs: out})
		if err != nil {
			t.Fatal(err)
		}
		if run.Failed {
			t.Fatalf("outputs %v: the run failed: %s", out, run.Err)
		}
		runs = append(runs, run)
	}
	skipped, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestOutputSkipped", Seed: 3})
	if err != nil || skipped.Failed {
		t.Fatalf("TestOutputSkipped: %v, %+v", err, skipped)
	}
	for _, c := range []struct {
		path, want string
		run        int
	}{{there, "to the host 42", 1}, {fresh, "to the host 42", 2}, {open, "left open", -1}} {
		b, err := os.ReadFile(c.path)
		if err != nil || string(b) != c.want {
			t.Errorf("%s holds %q (%v), want %q", c.path, b, err, c.want)
		}
		if c.run >= 0 && string(runs[c.run].Artifacts["back"]) != c.want {
			t.Errorf("run %d read back %q", c.run, runs[c.run].Artifacts["back"])
		}
	}
	for _, path := range []string{fresh, open} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("a new output %s: %v, %v; want mode 0600", path, info, err)
		}
	}
	for _, i := range []int{0, 3} {
		if b, ok := runs[i].Artifacts["back"]; ok {
			t.Errorf("run %d, given no output out, read back %q", i, b)
		}
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Errorf("an output the run did not make is on the host: %v", err)
	}
	for i, run := range runs[1:] {
		if !bytes.Equal(run.Checksum, runs[0].Checksum) || string(run.Artifacts["draws"]) != string(runs[0].Artifacts["draws"]) {
			t.Errorf("run %d: checksum %x, draws %q; without an output %x, %q", i+1, run.Checksum, run.Artifacts["draws"], runs[0].Checksum, runs[0].Artifacts["draws"])
		}
	}
	// Another test's checksum differs anyway; its draws come from the
	// same stream, which a draw before them would move.
	if string(skipped.Artifacts["draws"]) != string(runs[0].Artifacts["draws"]) {
		t.Errorf("TestOutputSkipped drew %q; TestOutput with no output %q", skipped.Artifacts["draws"], runs[0].Artifacts["draws"])
	}
	// An output whose file the host cannot make, in a directory that is
	// not there or at a directory, fails with the host's error, not as an
	// output the run was not given.
	for path, want := range map[string]string{filepath.Join(dir, "gone", "out"): "no such file or directory", dir: "is a directory"} {
		run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestOutput", Seed: 3, Outputs: map[string]string{"out": path}})
		if err != nil {
			t.Fatal(err)
		}
		if !run.Failed || !strings.Contains(string(run.LogOutput), want) {
			t.Errorf("a run given the output %s: failed %v, log %q, want %q", path, run.Failed, run.LogOutput, want)
		}
	}
}

// TestInputChangesNothing runs TestInput with an input and without: the
// run with it reads the input's bytes, and both draw the same values and
// end with the same checksum. A run given an input the test does not open
// plays out the same too.
func TestInputChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(path, []byte("from the host"), 0o600); err != nil {
		t.Fatal(err)
	}
	mt := metatesting.ForCurrentPackage(t)
	var runs []*metatesting.RunResult
	for _, in := range []map[string]string{nil, {"data": path}, {"other": path}} {
		run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestInput", Seed: 3, Inputs: in})
		if err != nil {
			t.Fatal(err)
		}
		if run.Failed {
			t.Fatalf("inputs %v: the run failed: %s", in, run.Err)
		}
		runs = append(runs, run)
	}
	if got := string(runs[1].Artifacts["input"]); got != "13 from the host" {
		t.Errorf("the run read %q from its input", got)
	}
	for _, i := range []int{0, 2} {
		if b, ok := runs[i].Artifacts["input"]; ok {
			t.Errorf("run %d, given no input data, read %q", i, b)
		}
	}
	// An input whose file the host cannot open fails the open with the
	// host's error, not as an input the run was not given.
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestInput", Seed: 3, Inputs: map[string]string{"data": filepath.Join(t.TempDir(), "gone")}})
	if err != nil {
		t.Fatal(err)
	}
	if !run.Failed || !strings.Contains(string(run.LogOutput), "no such file or directory") {
		t.Errorf("a run given an input whose file is gone: failed %v, log %q", run.Failed, run.LogOutput)
	}
	for i, run := range runs[1:] {
		if !bytes.Equal(run.Checksum, runs[0].Checksum) || string(run.Artifacts["draws"]) != string(runs[0].Artifacts["draws"]) {
			t.Errorf("run %d: checksum %x, draws %q; without an input %x, %q", i+1, run.Checksum, run.Artifacts["draws"], runs[0].Checksum, runs[0].Artifacts["draws"])
		}
	}
}

func TestWallLimit(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestWallLimit", Seed: 1, ExtraEnv: []string{"TESTINGWALL=1"}, WallLimit: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if run.Failed || !strings.Contains(string(run.Artifacts["stacks"]), "TestWallLimit") {
		t.Errorf("got failed %v, stacks %q", run.Failed, run.Artifacts["stacks"])
	}
	// The limit ends with its run.
	run, err = mt.RunQuiet(&metatesting.RunConfig{Test: "TestWriteArtifact", Seed: 1})
	if err != nil || run.Failed {
		t.Errorf("the next run: %v, failed %v", err, run != nil && run.Failed)
	}
}

func TestWallKill(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestWallKill", Seed: 1, ExtraEnv: []string{"TESTINGWALL=1"}, WallKill: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if !run.Failed || !run.Ended || string(run.Artifacts["before.txt"]) != "saved" || !strings.Contains(string(run.Artifacts["gosim-stacks.txt"]), "TestWallKill") || !strings.Contains(run.Err, "TestWallKill with seed 1") {
		t.Errorf("got failed %v, ended %v, err %q, artifacts %q", run.Failed, run.Ended, run.Err, slices.Sorted(maps.Keys(run.Artifacts)))
	}
	// The next run starts a new test binary.
	run, err = metatesting.ForCurrentPackage(t).RunQuiet(&metatesting.RunConfig{Test: "TestWriteArtifact", Seed: 1})
	if err != nil || run.Failed || string(run.Artifacts["artifact.txt"]) != "hello" {
		t.Errorf("the next run: %v, %+v", err, run)
	}
}

func TestUnimplementedHook(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestUnimplementedHook", Seed: 1, ExtraEnv: []string{"TESTINGFAIL=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "unimplemented hook: reflect.MapOf"; !run.Failed || run.Err != want {
		t.Errorf("got failed %v, err %q; want err %q", run.Failed, run.Err, want)
	}
}

func TestFatalUnlock(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestFatalUnlock", Seed: 1, ExtraEnv: []string{"TESTINGFAIL=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "fatal error: sync: unlock of unlocked mutex"; !run.Failed || run.Err != want {
		t.Errorf("got failed %v, err %q; want err %q", run.Failed, run.Err, want)
	}
}

func TestEmbedBytesPerRun(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	for range 2 {
		run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestEmbedBytesPerRun", Seed: 1})
		if err != nil {
			t.Fatal(err)
		}
		if run.Failed {
			t.Errorf("a run failed: %s", run.LogOutput)
		}
	}
}

func TestMaphashComparable(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestMaphashComparable", Seed: 1, ExtraEnv: []string{"TESTINGFAIL=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "unimplemented hook: hash/maphash.comparableHashUnimplemented"; !run.Failed || run.Err != want {
		t.Errorf("got failed %v, err %q; want err %q\n%s", run.Failed, run.Err, want, run.LogOutput)
	}
}
