package usecase

import (
	"context"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// GarageFields are the settable garage fields; nil fields are left
// unchanged (on create: defaulted). Empty optional strings clear them.
type GarageFields struct {
	Name                      *string
	TaxCode                   *string
	Address                   *string
	Phone                     *string
	Email                     *string
	Timezone                  *string
	Currency                  *string
	Status                    *domain.GarageStatus
	OperatingHours            *[]domain.DailyHours
	QuoteNumberTemplate       *string
	RepairOrderNumberTemplate *string
	SettlementNumberTemplate  *string
	// PaymentVoucherNumberTemplate numbers payment vouchers (FR-BIL-05).
	PaymentVoucherNumberTemplate *string
	// GoodsReceiptNumberTemplate and StockIssueNumberTemplate number goods
	// receipts and stock issues (FR-INV-06/20).
	GoodsReceiptNumberTemplate *string
	StockIssueNumberTemplate   *string
	// StockIssueMode is when a repair order's draft stock issue posts.
	StockIssueMode           *domain.StockIssueMode
	DefaultQuoteApprovalMode *domain.QuoteApprovalMode
	// Legal profile (FR-TEN-06): a blank value clears it, and a non-nil Bank
	// replaces the whole account.
	LegalName      *string
	LegalAddress   *string
	Representative *string
	Bank           *domain.BankDetails
}

// GarageWithAccess is a garage and what the caller may do in it.
type GarageWithAccess struct {
	Garage *domain.Garage
	Access *access.Access
}

// ListGarages returns the garages the user owns or works in, each with
// the caller's access.
func (uc *TenantUseCase) ListGarages(ctx context.Context, userID string) ([]GarageWithAccess, error) {
	garages, err := uc.Garages.ListForUser(ctx, userID)
	if err != nil {
		return nil, toAppError("list garages", err)
	}

	result := make([]GarageWithAccess, 0, len(garages))

	for _, g := range garages {
		user, err := uc.Access.GarageAccess(ctx, g.ID, userID)
		if err != nil {
			return nil, toAppError("load garage access", err)
		}

		result = append(result, GarageWithAccess{Garage: g, Access: user})
	}

	return result, nil
}

// CreateGarage creates a garage owned by ownerID (FR-TEN-01/02) within the
// plan's garage limit, with the owner's membership and one staff group per
// default role, and publishes garage.created.
func (uc *TenantUseCase) CreateGarage(ctx context.Context, ownerID string, fields GarageFields) (*GarageWithAccess, error) {
	g := &domain.Garage{
		ID:                           uuid.NewString(),
		OwnerUserID:                  ownerID,
		Timezone:                     domain.DefaultTimezone,
		Currency:                     domain.DefaultCurrency,
		Status:                       domain.GarageStatusActive,
		OperatingHours:               domain.DefaultOperatingHours(),
		QuoteNumberTemplate:          domain.DefaultQuoteNumberTemplate,
		RepairOrderNumberTemplate:    domain.DefaultRepairOrderNumberTemplate,
		SettlementNumberTemplate:     domain.DefaultSettlementNumberTemplate,
		PaymentVoucherNumberTemplate: domain.DefaultPaymentVoucherNumberTemplate,
		GoodsReceiptNumberTemplate:   domain.DefaultGoodsReceiptNumberTemplate,
		StockIssueNumberTemplate:     domain.DefaultStockIssueNumberTemplate,
		StockIssueMode:               domain.StockIssueModeOnClose,
		DefaultQuoteApprovalMode:     domain.QuoteApprovalModeLink,
		CreatedBy:                    &ownerID,
		UpdatedBy:                    &ownerID,
	}

	fields.Status = nil
	if fields.Name == nil {
		return nil, apperror.Validation("name", apperror.IssueRequired, "garage name is required")
	}

	if err := applyGarageFields(g, fields); err != nil {
		return nil, err
	}

	if err := uc.checkUsage(ctx, ownerID, LimitGarages); err != nil {
		return nil, err
	}

	err := uc.Tx.WithTransaction(ctx, platformdb.UserScope(ownerID).Garage(g.ID), func(ctx context.Context) error {
		if err := uc.EnsureDefaultRoles(ctx, ownerID, uc.userLocale(ctx, ownerID)); err != nil {
			return err
		}

		if err := uc.Garages.Create(ctx, g); err != nil {
			return err
		}

		owner := &domain.Staff{GarageID: g.ID, UserID: ownerID, Status: domain.StaffStatusActive}
		if err := uc.Staff.Create(ctx, owner); err != nil {
			return err
		}

		if err := uc.ensureDefaultGroups(ctx, g.ID, ownerID); err != nil {
			return err
		}

		if err := uc.audit(ctx, g.ID, "garage.created", "garage", g.ID, nil, g); err != nil {
			return err
		}

		return uc.Events.Publish(ctx, EventGarageCreated, g.ID, ownerID, map[string]any{
			keyGarageID: g.ID, "owner_user_id": ownerID, "name": g.Name,
		})
	})
	if err != nil {
		return nil, toAppError("create garage", err)
	}

	return &GarageWithAccess{Garage: g, Access: &access.Access{
		GarageID: g.ID, UserID: ownerID, OwnerUserID: ownerID, Owner: true, Active: true,
	}}, nil
}

// GetGarage returns a garage the caller was authorized for.
func (uc *TenantUseCase) GetGarage(ctx context.Context, garageID string) (*GarageWithAccess, error) {
	g, err := uc.Garages.FindByID(ctx, garageID)
	if err != nil {
		return nil, toAppError("get garage", err)
	}

	user, _ := access.FromContext(ctx)

	return &GarageWithAccess{Garage: g, Access: user}, nil
}

// UpdateGarage changes a garage's profile and configuration (FR-TEN-01/05).
// Only the owner may (de)activate it; reactivation counts against the plan.
func (uc *TenantUseCase) UpdateGarage(ctx context.Context, garageID string, fields GarageFields) (*GarageWithAccess, error) {
	caller, _ := access.FromContext(ctx)

	g, err := uc.Garages.FindByID(ctx, garageID)
	if err != nil {
		return nil, toAppError("update garage", err)
	}

	before := *g

	if fields.Status != nil && *fields.Status != g.Status {
		if caller == nil || !caller.Owner {
			return nil, apperror.New(apperror.CodePermissionDenied, access.ErrOwnerOnly.Error()).WithReason("OWNER_ONLY")
		}

		if *fields.Status == domain.GarageStatusActive {
			if err := uc.checkUsage(ctx, g.OwnerUserID, LimitGarages); err != nil {
				return nil, err
			}
		}
	}

	if err := applyGarageFields(g, fields); err != nil {
		return nil, err
	}

	actor := callerID(caller)
	g.UpdatedBy = &actor

	if err := uc.Garages.Update(ctx, g); err != nil {
		return nil, toAppError("update garage", err)
	}

	if err := uc.audit(ctx, g.ID, "garage.updated", "garage", g.ID, before, g); err != nil {
		return nil, toAppError("audit garage update", err)
	}

	return &GarageWithAccess{Garage: g, Access: caller}, nil
}

// LogoURL returns the public URL of a garage's logo key, nil without one.
func (uc *TenantUseCase) LogoURL(key *string) *string {
	if key == nil {
		return nil
	}

	url := uc.Logos.PublicURL(*key)

	return &url
}

// UploadGarageLogo replaces the garage logo (PNG, JPEG or WebP, at most
// 1 MB); the old file is removed once the change is committed.
func (uc *TenantUseCase) UploadGarageLogo(ctx context.Context, garageID string, content []byte, contentType string) (*GarageWithAccess, error) {
	caller, _ := access.FromContext(ctx)

	key, err := uc.Logos.StoreLogo(ctx, garageID, callerID(caller), content, strings.ToLower(strings.TrimSpace(contentType)))
	if err != nil {
		return nil, toAppError("store logo", err)
	}

	result, err := uc.setLogo(ctx, garageID, &key)
	if err != nil {
		uc.Logos.Discard(context.WithoutCancel(ctx), key)

		return nil, err
	}

	return result, nil
}

// DeleteGarageLogo removes the garage logo.
func (uc *TenantUseCase) DeleteGarageLogo(ctx context.Context, garageID string) (*GarageWithAccess, error) {
	return uc.setLogo(ctx, garageID, nil)
}

func (uc *TenantUseCase) setLogo(ctx context.Context, garageID string, key *string) (*GarageWithAccess, error) {
	caller, _ := access.FromContext(ctx)

	g, err := uc.Garages.FindByID(ctx, garageID)
	if err != nil {
		return nil, toAppError("update logo", err)
	}

	old := g.LogoKey
	g.LogoKey = key
	actor := callerID(caller)
	g.UpdatedBy = &actor

	if err := uc.Garages.Update(ctx, g); err != nil {
		return nil, toAppError("update logo", err)
	}

	if old != nil {
		if err := uc.Logos.Retire(ctx, garageID, actor, *old); err != nil {
			slog.Warn("tenant: retiring the old logo failed", "key", *old, "error", err)
		}
	}

	return &GarageWithAccess{Garage: g, Access: caller}, nil
}

// checkUsage enforces limit for ownerID against the current usage.
func (uc *TenantUseCase) checkUsage(ctx context.Context, ownerID string, limit Limit) error {
	garages, staff, err := uc.Usage.OwnerUsage(ctx, ownerID)
	if err != nil {
		return toAppError("read plan usage", err)
	}

	current := garages
	if limit == LimitStaff {
		current = staff
	}

	return uc.Limits.CheckLimit(ctx, ownerID, limit, current)
}

func callerID(caller *access.Access) string {
	if caller == nil {
		return ""
	}

	return caller.UserID
}

// CallerIDForTest is a test helper for callerID.
func CallerIDForTest(caller *access.Access) string {
	return callerID(caller)
}

// applyGarageFields validates and applies fields to g.
func applyGarageFields(g *domain.Garage, f GarageFields) error {
	if f.Name != nil {
		name := strings.TrimSpace(*f.Name)
		if !domain.ValidName(name) {
			return apperror.Validation("name", apperror.IssueRequired, "garage name is required (at most 255 characters)")
		}

		g.Name = name
	}

	if f.TaxCode != nil {
		taxCode := optional(*f.TaxCode)
		if taxCode != nil && len(*taxCode) > domain.MaxTaxCodeLength {
			return apperror.Validation("taxCode", apperror.IssueTooLong, "tax code is at most 20 characters")
		}

		g.TaxCode = taxCode
	}

	if f.Address != nil {
		g.Address = optional(*f.Address)
	}

	if err := applyContact(g, f); err != nil {
		return err
	}

	if err := applyLegalProfile(g, f); err != nil {
		return err
	}

	return applyConfiguration(g, f)
}

// applyLegalProfile sets the garage's legal identity (FR-TEN-06).
func applyLegalProfile(g *domain.Garage, f GarageFields) error {
	for _, field := range []struct {
		name  string
		value *string
		dst   **string
		max   int
	}{
		{"legalName", f.LegalName, &g.LegalName, domain.MaxLegalNameLength},
		{"legalAddress", f.LegalAddress, &g.LegalAddress, domain.MaxLegalAddressLength},
		{"representative", f.Representative, &g.Representative, domain.MaxLegalNameLength},
	} {
		if field.value == nil {
			continue
		}

		value := optional(*field.value)
		if value != nil && utf8.RuneCountInString(*value) > field.max {
			return apperror.Validation(field.name, apperror.IssueTooLong, field.name+" is too long")
		}

		*field.dst = value
	}

	if f.Bank == nil {
		return nil
	}

	bank := domain.BankDetails{}

	for _, field := range []struct {
		name string
		in   *string
		out  **string
	}{
		{"bank.bankName", f.Bank.BankName, &bank.BankName},
		{"bank.accountNumber", f.Bank.AccountNumber, &bank.AccountNumber},
		{"bank.accountHolder", f.Bank.AccountHolder, &bank.AccountHolder},
	} {
		if field.in == nil {
			continue
		}

		value := optional(*field.in)
		if value != nil && utf8.RuneCountInString(*value) > domain.MaxBankFieldLength {
			return apperror.Validation(field.name, apperror.IssueTooLong, field.name+" is too long")
		}

		*field.out = value
	}

	g.Bank = bank

	return nil
}

func applyContact(g *domain.Garage, f GarageFields) error {
	if f.Phone != nil {
		phone := optional(*f.Phone)
		if phone != nil && len(*phone) > 20 {
			return apperror.Validation("phone", apperror.IssueInvalid, "phone number is invalid")
		}

		g.Phone = phone
	}

	if f.Email != nil {
		email := optional(strings.ToLower(*f.Email))
		if email != nil && (len(*email) > 255 || !strings.Contains(*email, "@")) {
			return apperror.Validation("email", apperror.IssueInvalid, "email is invalid")
		}

		g.Email = email
	}

	return nil
}

func applyConfiguration(g *domain.Garage, f GarageFields) error {
	if f.Timezone != nil {
		if !domain.ValidTimezone(*f.Timezone) {
			return apperror.Validation("timezone", apperror.IssueInvalid, "timezone is not a known IANA timezone")
		}

		g.Timezone = *f.Timezone
	}

	if f.Currency != nil {
		currency := strings.ToUpper(strings.TrimSpace(*f.Currency))
		if !domain.ValidCurrency(currency) {
			return apperror.Validation("currency", apperror.IssueInvalid, "currency must be an ISO 4217 code")
		}

		g.Currency = currency
	}

	if f.Status != nil {
		if !f.Status.IsValid() {
			return apperror.Validation("status", apperror.IssueInvalid, "status is invalid")
		}

		g.Status = *f.Status
	}

	if f.OperatingHours != nil {
		if !domain.ValidOperatingHours(*f.OperatingHours) {
			return apperror.Validation("operatingHours", apperror.IssueInvalid, "operating hours are invalid")
		}

		g.OperatingHours = *f.OperatingHours
	}

	return applyDocumentConfiguration(g, f)
}

// applyDocumentConfiguration sets the fields FR-TEN-05 adds for each
// document-numbered module (Quote, Repair Order, Settlement) and the
// quote default approval mode. Split out of applyConfiguration to keep
// its cyclomatic complexity down.
func applyDocumentConfiguration(g *domain.Garage, f GarageFields) error {
	if f.QuoteNumberTemplate != nil {
		if !domain.ValidNumberingTemplate(*f.QuoteNumberTemplate) {
			return apperror.Validation("numbering.quote", apperror.IssueInvalid, "numbering template is invalid")
		}

		g.QuoteNumberTemplate = *f.QuoteNumberTemplate
	}

	if f.RepairOrderNumberTemplate != nil {
		if !domain.ValidNumberingTemplate(*f.RepairOrderNumberTemplate) {
			return apperror.Validation("numbering.repairOrder", apperror.IssueInvalid, "numbering template is invalid")
		}

		g.RepairOrderNumberTemplate = *f.RepairOrderNumberTemplate
	}

	if f.SettlementNumberTemplate != nil {
		if !domain.ValidNumberingTemplate(*f.SettlementNumberTemplate) {
			return apperror.Validation("numbering.settlement", apperror.IssueInvalid, "numbering template is invalid")
		}

		g.SettlementNumberTemplate = *f.SettlementNumberTemplate
	}

	if f.PaymentVoucherNumberTemplate != nil {
		if !domain.ValidNumberingTemplate(*f.PaymentVoucherNumberTemplate) {
			return apperror.Validation("numbering.paymentVoucher", apperror.IssueInvalid, "numbering template is invalid")
		}

		g.PaymentVoucherNumberTemplate = *f.PaymentVoucherNumberTemplate
	}

	if err := applyStockConfiguration(g, f); err != nil {
		return err
	}

	if f.DefaultQuoteApprovalMode != nil {
		if !f.DefaultQuoteApprovalMode.IsValid() {
			return apperror.Validation("defaultQuoteApprovalMode", apperror.IssueInvalid, "quote approval mode is invalid")
		}

		g.DefaultQuoteApprovalMode = *f.DefaultQuoteApprovalMode
	}

	return nil
}

// optional trims s and returns nil for "".
func optional(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	return &s
}

// applyStockConfiguration sets the Release 7.0 fields: the goods receipt and
// stock issue numbering templates (FR-TEN-05) and when a stock issue posts
// (FR-INV-20).
func applyStockConfiguration(g *domain.Garage, f GarageFields) error {
	if f.GoodsReceiptNumberTemplate != nil {
		if !domain.ValidNumberingTemplate(*f.GoodsReceiptNumberTemplate) {
			return apperror.Validation("numbering.goodsReceipt", apperror.IssueInvalid, "numbering template is invalid")
		}

		g.GoodsReceiptNumberTemplate = *f.GoodsReceiptNumberTemplate
	}

	if f.StockIssueNumberTemplate != nil {
		if !domain.ValidNumberingTemplate(*f.StockIssueNumberTemplate) {
			return apperror.Validation("numbering.stockIssue", apperror.IssueInvalid, "numbering template is invalid")
		}

		g.StockIssueNumberTemplate = *f.StockIssueNumberTemplate
	}

	if f.StockIssueMode != nil {
		if !f.StockIssueMode.IsValid() {
			return apperror.Validation("stockIssueMode", apperror.IssueInvalid, "stock issue mode is invalid")
		}

		g.StockIssueMode = *f.StockIssueMode
	}

	return nil
}
