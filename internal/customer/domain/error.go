package domain

import "errors"

// ReasonVehicleFieldLocked is the error reason of an edit of a vehicle's
// identity while a live document refers to it (FR-CUS-02).
const ReasonVehicleFieldLocked = "VEHICLE_FIELD_LOCKED"

// Sentinel errors for business-rule violations in the Customer & Vehicle
// module.
var (
	ErrCustomerNotFound = errors.New("customer not found")
	ErrVehicleNotFound  = errors.New("vehicle not found")

	ErrFullNameRequired     = errors.New("full name is required")
	ErrInvalidLocale        = errors.New("locale must be vi or en")
	ErrLicensePlateRequired = errors.New("license plate is required")
	ErrCustomerRequired     = errors.New("customer is required")

	ErrInvalidLicensePlate = errors.New("license plate must contain 4 to 12 letters or digits")
	ErrInvalidYear         = errors.New("model year is out of range")
	ErrFieldTooLong        = errors.New("field is too long")

	// ErrVehicleFieldLocked is returned when an identity field is edited
	// while a live document refers to the vehicle.
	ErrVehicleFieldLocked = errors.New("this vehicle is on a live document, so its identity can only be corrected")
	// ErrInvalidIdentityField is returned for a correction of a field that
	// does not identify a vehicle.
	ErrInvalidIdentityField = errors.New("the field is not an identity field of a vehicle")
	// ErrCorrectionReasonRequired is returned for a correction or an owner
	// change without its reason.
	ErrCorrectionReasonRequired = errors.New("a reason is required")
	// ErrOwnerNotChanged is returned when a vehicle is moved to its owner.
	ErrOwnerNotChanged = errors.New("the vehicle already belongs to this customer")
	// ErrOwnerChangeOnUpdate is returned when an update tries to change the
	// owner, which ChangeVehicleOwner alone does.
	ErrOwnerChangeOnUpdate = errors.New("the owner is changed with a change of owner, not an update")

	ErrPhoneTaken        = errors.New("another customer already uses this phone number")
	ErrLicensePlateTaken = errors.New("another vehicle already uses this license plate")
)
