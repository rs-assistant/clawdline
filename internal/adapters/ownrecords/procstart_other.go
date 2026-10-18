//go:build !darwin && !linux

package ownrecords

import "time"

// ProcessStart has no kernel reader on this platform. The zero time matches
// no recorded identity.
func ProcessStart(pid int) time.Time { return time.Time{} }
