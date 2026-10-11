package middleware_test

import "errors"

var (
	errMiddlewareDBDown     = errors.New("db down")
	errMiddlewareHandler    = errors.New("handler failed")
	errMiddlewareMustNotRun = errors.New("must not be called")
	errMiddlewareDB         = errors.New("db")
	errMiddlewareCacheDown  = errors.New("cache down")
)
