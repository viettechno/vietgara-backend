package usecase_test

import "errors"

var (
	errUnexpectedUser         = errors.New("unexpected user")
	errReadOutsideGarageScope = errors.New("read outside its garage scope")
)
