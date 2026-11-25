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
