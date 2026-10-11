package apperror_test

import "errors"

var (
	errApperrorDBDown   = errors.New("db down")
	errApperrorPostgres = errors.New("pg")
	errApperrorRaw      = errors.New("raw")
)
