//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package scanner

func isUnsupportedXAttrError(error) bool { return false }
