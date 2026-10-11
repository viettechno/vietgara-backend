package db_test

import "errors"

var (
	errIntegrationCallbackFailed      = errors.New("callback failed")
	errTransactionConnectionLost      = errors.New("connection lost")
	errTransactionDatabaseUnavailable = errors.New("database unavailable")
	errTransactionCallbackFailed      = errors.New("callback failed")
	errTransactionCommitFailed        = errors.New("commit failed")
	errTransactionBoom                = errors.New("boom")
	errTransactionFail                = errors.New("fail")
)
