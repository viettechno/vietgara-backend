package usecase

// keyGarageID is the garage key of event payloads.
const keyGarageID = "garage_id"

// Events published by the Tenant module (Event Design Section 4).
const (
	EventGarageCreated = "garage.created"
	EventStaffInvited  = "staff.invited"
	EventStaffJoined   = "staff.joined"
	EventStaffRemoved  = "staff.removed"
)
