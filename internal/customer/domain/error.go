package domain

import "errors"

// Sentinel errors for business-rule violations in the Customer & Vehicle
// module.
var (
	ErrCustomerNotFound = errors.New("customer not found")
	ErrVehicleNotFound  = errors.New("vehicle not found")

	ErrFullNameRequired     = errors.New("full name is required")
	ErrLicensePlateRequired = errors.New("license plate is required")
	ErrCustomerRequired     = errors.New("customer is required")

	ErrInvalidPhone        = errors.New("phone number must contain 8 to 15 digits")
	ErrInvalidEmail        = errors.New("email address is invalid")
	ErrInvalidLicensePlate = errors.New("license plate must contain 4 to 12 letters or digits")
	ErrInvalidYear         = errors.New("model year is out of range")
	ErrFieldTooLong        = errors.New("field is too long")

	ErrPhoneTaken        = errors.New("another customer already uses this phone number")
	ErrLicensePlateTaken = errors.New("another vehicle already uses this license plate")
)
