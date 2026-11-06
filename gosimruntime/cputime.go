package gosimruntime

import "syscall"

// CPUTime returns the CPU time the process has used so far, in
// nanoseconds, user and system, on all of its threads: the simulation's
// and the garbage collector's. A simulation reads it to measure what its
// work costs. Reading it changes nothing in the run. Its value differs
// from run to run, so the run must not act on it or log it.
func CPUTime() (int64, error) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, err
	}
	return ru.Utime.Nano() + ru.Stime.Nano(), nil
}
