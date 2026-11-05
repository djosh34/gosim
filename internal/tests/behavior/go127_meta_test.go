//go:build !sim

package behavior_test

import (
	"maps"
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
