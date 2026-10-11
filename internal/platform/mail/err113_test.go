package mail_test

import "errors"

var (
	errMailTestTemporary    = errors.New("421 try later")
	errMailTestProviderDown = errors.New("provider down")
)
