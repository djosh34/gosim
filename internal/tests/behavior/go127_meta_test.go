//go:build !sim

package behavior_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jellevandenhooff/gosim/internal/gosimtool"
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

// TestCPUKill runs the spinning TestWallKill with a limit of CPU time
// well before its limit of real time: the CPU limit ends it as WallKill
// would, with what it saved and every goroutine's stack.
func TestCPUKill(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestWallKill", Seed: 1, ExtraEnv: []string{"TESTINGWALL=1"}, CPUKill: 2 * time.Second, WallKill: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if !run.Failed || !run.Ended || string(run.Artifacts["before.txt"]) != "saved" || !strings.Contains(string(run.Artifacts["gosim-stacks.txt"]), "TestWallKill") || !strings.Contains(run.Err, "TestWallKill with seed 1 passed its limit of 2s of CPU time") {
		t.Errorf("got failed %v, ended %v, err %q, artifacts %q", run.Failed, run.Ended, run.Err, slices.Sorted(maps.Keys(run.Artifacts)))
	}
	// A run that ends well within its limit of CPU time passes, and a new
	// test binary runs it.
	run, err = metatesting.ForCurrentPackage(t).RunQuiet(&metatesting.RunConfig{Test: "TestWriteArtifact", Seed: 1, CPUKill: time.Hour})
	if err != nil || run.Failed || string(run.Artifacts["artifact.txt"]) != "hello" {
		t.Errorf("the next run: %v, %+v", err, run)
	}
}

func TestReflectLinkname(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestReflectLinkname", Seed: 1, ExtraEnv: []string{"TESTINGFAIL=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "unimplemented hook: github.com/jellevandenhooff/gosim/internal/tests/behavior/linkname.unsafeNew"; !run.Failed || run.Err != want {
		t.Errorf("got failed %v, err %q; want err %q", run.Failed, run.Err, want)
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

func TestForTestBinary(t *testing.T) {
	// A copy of the binary elsewhere, as a campaign's jobs download it.
	built := gosimtool.GetPathForPrecompiledTestBinary(t, "github.com/jellevandenhooff/gosim/internal/tests/behavior", nil)
	data, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "behavior.test")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	run, err := metatesting.ForTestBinary(t, path).RunQuiet(&metatesting.RunConfig{Test: "TestWriteArtifact", Seed: 1})
	if err != nil || run.Failed || string(run.Artifacts["artifact.txt"]) != "hello" {
		t.Errorf("got %v, %+v", err, run)
	}
}

func TestExitNonzero(t *testing.T) {
	run, err := metatesting.ForCurrentPackage(t).RunQuiet(&metatesting.RunConfig{Test: "TestExitNonzero", Seed: 1, ExtraEnv: []string{"TESTINGFAIL=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "os.Exit(3) on the main machine"; !run.Failed || !strings.Contains(run.Err, want) {
		t.Errorf("got failed %v, err %q; want err with %q", run.Failed, run.Err, want)
	}
}

func TestPanicLog(t *testing.T) {
	mt := metatesting.ForCurrentPackage(t)
	var checksums []string
	for range 2 {
		run, err := mt.RunQuiet(&metatesting.RunConfig{Test: "TestPanicLog", Seed: 1, ExtraEnv: []string{"TESTINGFAIL=1"}})
		if err != nil {
			t.Fatal(err)
		}
		if !run.Failed || !strings.Contains(string(run.LogOutput), "a panic for the log") {
			t.Fatalf("got failed %v, log %s", run.Failed, run.LogOutput)
		}
		checksums = append(checksums, string(run.Checksum))
	}
	if checksums[0] != checksums[1] {
		t.Errorf("two runs of a panic gave different checksums")
	}
}
