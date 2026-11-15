//go:build !darwin && !linux

package store

func fsType(dir string) (string, bool, error) { return "unknown", false, nil }
