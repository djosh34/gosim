package gosimruntime

import (
	"context"
	"encoding/binary"
	"log/slog"
)

type checksummer struct {
	step   int
	hash   fnv64
	logger *slog.Logger
	// level is the level of the checksum's log lines, and logs whether
	// the logger takes them, which a run does not change.
	level slog.Level
	logs  bool
}

func newChecksummer(logger *slog.Logger) *checksummer {
	level := slog.LevelDebug
	if *forceChecksum {
		level = slog.LevelError
	}
	return &checksummer{
		step:   0,
		hash:   newFnv64(),
		logger: logger,
		level:  level,
		logs:   logger.Enabled(context.TODO(), level),
	}
}

//go:generate go run golang.org/x/tools/cmd/stringer -type=checksumKey

type checksumKey byte

const (
	checksumKeyRunPick checksumKey = iota
	checksumKeyRunResult
	checksumKeyLogWrite
	checksumKeyCreating
	checksumKeyRunStarted
	checksumKeyRunFinished
	checksumKeyTimeNow
)

func (t *checksummer) recordIntInt(key checksumKey, a, b uint64) {
	if inControlledNondeterminism() {
		panic("help")
	}

	t.hash.HashWord(uint64(key))
	t.hash.HashWord(a)
	t.hash.HashWord(b)

	if t.logs {
		t.logger.LogAttrs(context.TODO(), t.level, "checksummer",
			slog.Int("step", t.step),
			slog.String("key", key.String()),
			slog.Int("a", int(a)),
			slog.Int("b", int(b)),
			slog.Int64("sum", int64(t.hash)))
	}
	t.step++
}

func (t *checksummer) recordBytes(key checksumKey, a []byte) {
	if inControlledNondeterminism() {
		panic("help")
	}

	t.hash.HashWord(uint64(key))
	t.hash.Hash(a)

	if t.logs {
		t.logger.LogAttrs(context.TODO(), t.level, "checksummer",
			slog.Int("step", t.step),
			slog.String("key", key.String()),
			slog.String("a", string(a)),
			slog.Int64("sum", int64(t.hash)))
	}
	t.step++
}

func (t *checksummer) finalize() []byte {
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(t.hash))
	return n[:]
}

type checksumWriter struct {
	checksummer *checksummer
}

func (t checksumWriter) Write(p []byte) (n int, err error) {
	t.checksummer.recordBytes(checksumKeyLogWrite, p)
	return len(p), nil
}
