package handler_test

import "context"

// noRelease is the release function of a document that was counted and then
// created: there is nothing to give back.
func noRelease(context.Context) {}
