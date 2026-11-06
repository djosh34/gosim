package gosim

import (
	"time"

	"github.com/jellevandenhooff/gosim/gosimruntime"
	"github.com/jellevandenhooff/gosim/internal/simulation"
)

// IsSim returns if the program is running inside of a gosim simulation.
//
// To include or exclude code or tests only if running inside of a gosim the
// gosim tool also supports a build tag. Use the `//go:build sim` build
// constraint at the top of a go file to only include it in simulated builds or
// `//go:build !sim` to exclude it from simulated from builds.
func IsSim() bool {
	return gosimruntime.IsSim()
}

// Yield yields the processor, allowing other goroutines to run. Similar to
// [runtime.Gosched] but for gosim's scheduler.
//
// TODO: Replace with runtime.Gosched() instead?
func Yield() {
	gosimruntime.Yield()
}

/*
type Simulation struct{}

func CurrentSimulation() Simulation {
	return Simulation{}
}

// SetConnected configures the network connectivity between addresses a and b.
//
// TODO: How will this fail? Unrouteable or rejected
// TODO: Support asymmetry
func (s Simulation) SetConnected(a, b string, connected bool) error {
	return simulation.SyscallSetConnected(a, b, connected)
}

// SetDelay configures the network latency between addresses a and b.
//
// TODO: Support asymmetry
func (s Simulation) SetDelay(a, b string, delay time.Duration) error {
	return simulation.SyscallSetDelay(a, b, delay)
}

// SetSimulationTimeout resets the simulation timeout to the given duration.
// After the simulated duration the simululation will fail with a test timeout
// error.
func (s Simulation) SetSimulationTimeout(timeout time.Duration) {
	err := simulation.SyscallSetSimulationTimeout(timeout)
	if err != nil {
		panic(err)
	}
}
*/

// SetConnected configures the network connectivity between addresses a and b.
//
// TODO: How will this fail? Unrouteable or rejected
// TODO: Support asymmetry
func SetConnected(a, b string, connected bool) error {
	return simulation.SyscallSetConnected(a, b, connected)
}

// SetDelay configures the network latency between addresses a and b.
//
// TODO: Support asymmetry
func SetDelay(a, b string, delay time.Duration) error {
	return simulation.SyscallSetDelay(a, b, delay)
}

// SetSimulationTimeout resets the simulation timeout to the given duration.
// After the simulated duration the simululation will fail with a test timeout
// error.
func SetSimulationTimeout(d time.Duration) {
	err := simulation.SyscallSetSimulationTimeout(d)
	if err != nil {
		panic(err)
	}
}

// WallLimitPassed reports whether the current run's limit of real time,
// set by the test that started it with metatesting.RunConfig.WallLimit, has
// passed. A simulation reads it where it can stop and report itself; until
// the limit passes it changes nothing in the run.
func WallLimitPassed() bool {
	return gosimruntime.WallLimitPassed()
}

// CPUTime returns the CPU time the process has used so far, user and
// system, on all of its threads: the simulation's and the garbage
// collector's. A simulation reads it to measure what its work costs; it
// changes nothing in the run, and the run must not let it change anything.
func CPUTime() (time.Duration, error) {
	ns, err := gosimruntime.CPUTime()
	return time.Duration(ns), err
}

// Stacks returns the stack of every goroutine of the process, the
// simulation's included, for the report of a run that went on too long.
func Stacks() []byte {
	return gosimruntime.Stacks()
}

// WriteArtifact saves a copy of data under name in the result of the current
// run, for the test that started the run: a test using metatesting reads it
// from RunResult.Artifacts. The simulation cannot read it back. Writing a name
// again replaces its data.
func WriteArtifact(name string, data []byte) {
	gosimruntime.WriteArtifact(name, data)
}
