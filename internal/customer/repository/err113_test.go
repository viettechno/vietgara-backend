package repository_test

import "errors"

var (
	errScanColumnCountMismatch = errors.New("scan: column count mismatch")
	errScanValueTypeMismatch   = errors.New("scan: value type mismatch")
)
