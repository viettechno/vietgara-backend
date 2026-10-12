package repository_test

import (
	"errors"
	"time"
)

const (
	garageID   = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	quoteID    = "11111111-1111-1111-1111-111111111111"
	customerID = "22222222-2222-2222-2222-222222222222"
	vehicleID  = "33333333-3333-3333-3333-333333333333"
	lineID     = "44444444-4444-4444-4444-444444444444"
	actorID    = "55555555-5555-5555-5555-555555555555"
)

var (
	now     = time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

// quoteRow matches the column order of quoteColumns in postgres.go.
func quoteRow() []any {
	return []any{
		quoteID, garageID, customerID, vehicleID, "An Nguyen", "51F-123.45", "BG-202609-0001", "draft", "link",
		"", int64(0), []byte(`{"name":"Cong ty An Phat","tax_code":"0301234567"}`), []byte(`{"tax_profile_id":"tp-1","legal_name":"Cong ty Van Tai A"}`),
		nil, nil, nil, nil, nil, actorID, actorID, now, now,
	}
}

// lineRow matches the column order of lineColumns in postgres.go.
func lineRow() []any {
	return []any{lineID, 1, "labor", nil, "Oil change", "1", int64(250_000), int64(250_000), "pending", nil}
}
