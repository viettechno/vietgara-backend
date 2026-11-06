package usecase

import "time"

// SetClock replaces the clock of a PublicUseCase in tests.
func SetClock(uc *PublicUseCase, now func() time.Time) { uc.now = now }

// SetStaffClock replaces the clock of a StaffUseCase in tests.
func SetStaffClock(uc *StaffUseCase, now func() time.Time) { uc.now = now }
