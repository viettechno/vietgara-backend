// Package ptr provides generic pointer utilities.
package ptr

// Deref returns the value pointed to by ptr, or def if ptr is nil.
func Deref[T any](ptr *T, def T) T {
	if ptr != nil {
		return *ptr
	}

	return def
}

// To returns a pointer to v.
func To[T any](v T) *T {
	return new(v)
}
