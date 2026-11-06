package domain

import "errors"

// ReasonVehicleFieldLocked is the error reason of an edit of a vehicle's
// identity while a live document refers to it (FR-CUS-02).
const ReasonVehicleFieldLocked = "VEHICLE_FIELD_LOCKED"

// ReasonCustomerAnonymized is the error reason of a change of an anonymized
// customer (FR-CUS-05).
const ReasonCustomerAnonymized = "CUSTOMER_ANONYMIZED"

// AnonymizedName replaces the name of a customer whose data was erased
// (PD-06); the plate of a vehicle becomes AnonymizedPlatePrefix and a part of
// its id.
const (
	AnonymizedName        = "Khách hàng đã ẩn danh"
	AnonymizedPlatePrefix = "ANON-"
)

// AnonymizedCounts is what an anonymization changed.
type AnonymizedCounts struct {
	Customers   int
	Vehicles    int
	TaxProfiles int
}

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

	// ErrCustomerAnonymized is returned for changing a customer whose personal
	// data was erased: it cannot be edited or given vehicles.
	ErrCustomerAnonymized = errors.New("the customer was anonymized and can no longer be changed")

	ErrPhoneTaken        = errors.New("another customer already uses this phone number")
	ErrLicensePlateTaken = errors.New("another vehicle already uses this license plate")
)
