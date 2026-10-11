package handler

import (
	"math"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"

	"github.com/viettechno/vietgara-backend/internal/supplier/domain"
	"github.com/viettechno/vietgara-backend/internal/supplier/usecase"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func supplierToProto(s *domain.Supplier) *supplierv1.Supplier {
	return &supplierv1.Supplier{
		Id:       s.ID,
		GarageId: s.GarageID,
		Name:     s.Name,
		TaxCode:  s.TaxCode,
		Contact: &supplierv1.SupplierContact{
			ContactPerson: s.Contact.ContactPerson,
			Phone:         s.Contact.Phone,
			Email:         s.Contact.Email,
			Address:       s.Contact.Address,
		},
		Bank: &commonv1.BankDetails{
			BankName:      s.Bank.BankName,
			AccountNumber: s.Bank.AccountNumber,
			AccountHolder: s.Bank.AccountHolder,
		},
		Note:      s.Note,
		IsActive:  s.IsActive,
		CreatedAt: converter.TimeToProto(s.CreatedAt),
		UpdatedAt: converter.TimeToProto(s.UpdatedAt),
	}
}

// contactFromProto maps an absent contact to an empty one.
func contactFromProto(c *supplierv1.SupplierContact) usecase.ContactInput {
	if c == nil {
		return usecase.ContactInput{}
	}

	return usecase.ContactInput{
		ContactPerson: c.ContactPerson,
		Phone:         c.Phone,
		Email:         c.Email,
		Address:       c.Address,
	}
}

// bankFromProto maps an absent bank block to an empty one.
func bankFromProto(b *commonv1.BankDetails) usecase.BankInput {
	if b == nil {
		return usecase.BankInput{}
	}

	return usecase.BankInput{BankName: b.BankName, AccountNumber: b.AccountNumber, AccountHolder: b.AccountHolder}
}

func paginationToProto[T any](p *usecase.Page[T]) *commonv1.Pagination {
	return &commonv1.Pagination{Total: toInt32(p.Total), Page: toInt32(p.Page), PageSize: toInt32(p.PageSize)}
}

func pageRequest(page, pageSize int32) usecase.PageRequest {
	return usecase.PageRequest{Page: int(page), PageSize: int(pageSize)}
}

func toInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}

	if n < math.MinInt32 {
		return math.MinInt32
	}

	return int32(n)
}
