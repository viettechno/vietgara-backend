package usecase_test

import "context"

// noRelease is the release function of a document that was counted and then
// created: there is nothing to give back.
func noRelease(context.Context) {}

// recordRelease is the release function of a document that is not created
// after all: the test checks that it was called.
func recordRelease(released *bool) func(context.Context) {
	return func(context.Context) { *released = true }
}
