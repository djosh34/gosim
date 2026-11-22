// Package initflag records that the initializers of packages initdep and
// initdot ran.
package initflag

// Set is true once initdep's init has run, and DotSet once initdot's has.
var Set, DotSet bool
