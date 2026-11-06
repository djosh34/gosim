package gosimruntime

import "syscall"

// CPUTime returns the CPU time the process has used so far, in
// nanoseconds, user and system, on all of its threads: the simulation's
// and the garbage collector's. A simulation reads it to measure what its
// work costs, and must never let it change what the run does.
func CPUTime() (int64, error) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, err
	}
	return ru.Utime.Nano() + ru.Stime.Nano(), nil
}
