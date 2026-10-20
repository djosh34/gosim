package go127

import (
	"time"

	"github.com/jellevandenhooff/gosim/gosimruntime"
)

func RuntimeDebug_WriteHeapDump(fd uintptr) {
	gosimruntime.Unimplemented("runtime/debug.WriteHeapDump")
}

func RuntimeDebug_SetTraceback(level string) {
	gosimruntime.Unimplemented("runtime/debug.SetTraceback")
}

func RuntimeDebug_modinfo() string {
	gosimruntime.Unimplemented("runtime/debug.modinfo")
	panic("unreachable")
}

func RuntimeDebug_readGCStats(*[]time.Duration) {
	gosimruntime.Unimplemented("runtime/debug.readGCStats")
}

func RuntimeDebug_freeOSMemory() {
	gosimruntime.Unimplemented("runtime/debug.freeOSMemory")
}

func RuntimeDebug_setMaxStack(int) int {
	gosimruntime.Unimplemented("runtime/debug.setMaxStack")
	panic("unreachable")
}

func RuntimeDebug_setGCPercent(int32) int32 {
	gosimruntime.Unimplemented("runtime/debug.setGCPercent")
	panic("unreachable")
}

func RuntimeDebug_setPanicOnFault(bool) bool {
	gosimruntime.Unimplemented("runtime/debug.setPanicOnFault")
	panic("unreachable")
}

func RuntimeDebug_setMaxThreads(int) int {
	gosimruntime.Unimplemented("runtime/debug.setMaxThreads")
	panic("unreachable")
}

func RuntimeDebug_setMemoryLimit(int64) int64 {
	gosimruntime.Unimplemented("runtime/debug.setMemoryLimit")
	panic("unreachable")
}
