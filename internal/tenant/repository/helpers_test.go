package repository_test

import (
	"errors"
	"time"
)

var (
	errBoom = errors.New("boom")
	now     = time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
)

func garageRow() []any {
	return []any{
		"g-1", "owner", "Gara", "0101", "Gara Co., Ltd", nil, nil, []byte(`{"bank_name":"VCB","account_number":"001"}`),
		nil, nil, "a@b.vn", nil, "Asia/Ho_Chi_Minh", "VND", "active",
		[]byte(`[{"day":1,"closed":false,"open":"08:00","close":"17:00"}]`), "BG-{SEQ:4}", "LSC-{SEQ:4}", "QT-{SEQ:4}",
		"PT-{SEQ:4}", "link", "owner", nil, now, now,
	}
}

func staffRow() []any {
	return []any{"s-1", "g-1", "user-1", "active", false, []string{"grp-1"}, []string{"report.read", "customer.read"}, now, now}
}

func roleRow() []any {
	return []any{"r-1", "owner", "Kế toán", "desc", "accountant", []string{"billing.read"}, 2, now, now}
}

func groupRow() []any {
	return []any{"grp-1", "g-1", "Kế toán", "", "r-1", "Kế toán", nil, 3, now, now}
}

func invitationRow() []any {
	return []any{"i-1", "g-1", "user-1", []string{"grp-1"}, "pending", "owner", now, nil, now, now, "Gara"}
}
