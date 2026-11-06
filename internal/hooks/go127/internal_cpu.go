package go127

import "github.com/jellevandenhooff/gosim/gosimruntime"

func InternalCpu_gosimGODEBUG() string {
	return gosimruntime.GODEBUG()
}

func InternalCpu_gosimHWCap() uint {
	return gosimruntime.HWCap()
}
