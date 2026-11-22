// Package aliased is imported under another name by a test whose folded
// constant names it, while the test still uses the import after the fold.
package aliased

// T is the type the test names.
type T int
