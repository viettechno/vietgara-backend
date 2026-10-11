// Package lifecycle implements the delete-or-deactivate rule (FR-CAT-07):
// deleting an object that nothing references soft-deletes it, while an
// object that is still in use is only deactivated, so the documents that
// use it stay intact and staff can reactivate it later.
package lifecycle

import "context"

// Outcome is what a delete did, or would do.
type Outcome string

// The two outcomes of a delete.
const (
	// Deleted: nothing referenced the object, so it was soft-deleted.
	Deleted Outcome = "deleted"
	// Deactivated: the object is in use, so it was only deactivated.
	Deactivated Outcome = "deactivated"
)

// Rule describes how to delete one object. Each module supplies its own
// reference check and persistence calls; the decision itself lives here.
type Rule struct {
	// InUse reports whether anything still references the object.
	InUse func(ctx context.Context) (bool, error)
	// Delete soft-deletes the object (and whatever is owned by it).
	Delete func(ctx context.Context) error
	// Deactivate marks the object inactive.
	Deactivate func(ctx context.Context) error
}

// Apply decides the outcome for the object and, unless dryRun is set,
// carries it out. A dry run only answers what would happen, so a
// confirmation dialog can tell the user which one it will be.
func Apply(ctx context.Context, dryRun bool, rule Rule) (Outcome, error) {
	inUse, err := rule.InUse(ctx)
	if err != nil {
		return "", err
	}

	outcome := Deleted
	act := rule.Delete

	if inUse {
		outcome = Deactivated
		act = rule.Deactivate
	}

	if dryRun {
		return outcome, nil
	}

	if err := act(ctx); err != nil {
		return "", err
	}

	return outcome, nil
}

// AnyInUse reports whether any of the checks does, stopping at the first
// that does. Checks that read other modules' data are passed in by the
// wiring, so the owning module never imports them.
func AnyInUse(ctx context.Context, checks ...func(ctx context.Context) (bool, error)) (bool, error) {
	for _, check := range checks {
		inUse, err := check(ctx)
		if err != nil {
			return false, err
		}

		if inUse {
			return true, nil
		}
	}

	return false, nil
}
