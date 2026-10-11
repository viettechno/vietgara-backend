package usecase_test

import "errors"

var (
	errTestPurgeInvitedBy = errors.New("invited_by")
	errTestPurgeDBDown    = errors.New("db down")
)
