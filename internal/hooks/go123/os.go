package go123

import "github.com/jellevandenhooff/gosim/gosimruntime"

func Os_runtime_args() []string {
	// TODO: make this configurable / fetch this from the machine API?
	return []string{"gosimapp"}
}

func Os_sigpipe() {
	gosimruntime.Unimplemented("os.sigpipe")
}

func Os_runtime_beforeExit(exitCode int) {
	gosimruntime.Unimplemented("os.runtime_beforeExit")
}

func Os_runtime_rand() uint64 {
	return gosimruntime.Fastrand64()
}

func Os_checkClonePidfd() error {
	gosimruntime.Unimplemented("os.checkClonePidfd")
	panic("unreachable")
}

func Os_ignoreSIGSYS() {
	gosimruntime.Unimplemented("os.ignoreSIGSYS")
}

func Os_restoreSIGSYS() {
	gosimruntime.Unimplemented("os.restoreSIGSYS")
}
