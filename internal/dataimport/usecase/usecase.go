// Package usecase contains the application service of the Data Import module
// (FR-INV-14). It reads a workbook, validates every row without writing
// anything, and on confirm writes the whole file through the Catalogue and
// Inventory use cases in the caller's transaction. The import is
// all-or-nothing: one invalid row refuses the file.
package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
	"github.com/viettechno/vietgara-backend/internal/dataimport/template"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Audit action and entity type of a confirmed import.
const (
	ActionImportCompleted = "import.completed"
	entityImportRun       = "import_run"
)

// Permissions says what the caller may do; the handler reads them from the
// request's garage access.
type Permissions struct {
	CatalogueWrite    bool
	InventoryWrite    bool
	InventoryCostRead bool
	InventoryCount    bool
}

// Dependencies wires the use case.
type Dependencies struct {
	Catalogue Catalogue
	Suppliers Suppliers
	Inventory Inventory
	Runs      RunRepository
	Reader    Reader
	Audit     AuditLogger
	Now       func() time.Time
}

// UseCase is the Data Import module's application service.
type UseCase struct {
	catalogue Catalogue
	suppliers Suppliers
	inventory Inventory
	runs      RunRepository
	reader    Reader
	audit     AuditLogger
	now       func() time.Time
}

// NewUseCase builds the service; a nil Now means time.Now.
func NewUseCase(d Dependencies) *UseCase {
	now := d.Now
	if now == nil {
		now = time.Now
	}

	return &UseCase{catalogue: d.Catalogue, suppliers: d.Suppliers, inventory: d.Inventory, runs: d.Runs, reader: d.Reader, audit: d.Audit, now: now}
}

// Request is the input of Preview and Confirm.
type Request struct {
	GarageID string
	ActorID  string
	Type     domain.Type
	FileName string
	Content  []byte
	// Permissions is checked against the type and the columns the file uses.
	Permissions Permissions
}

// DownloadTemplate renders the workbook of typ with its file name.
func (uc *UseCase) DownloadTemplate(typ domain.Type) ([]byte, string, error) {
	if _, ok := domain.ParseType(string(typ)); !ok {
		return nil, "", toAppError("download import template", domain.ErrUnsupportedType)
	}

	data, name, err := template.Build(typ)
	if err != nil {
		return nil, "", toAppError("download import template", err)
	}

	return data, name, nil
}

// Preview checks the file and reports every error without writing anything.
func (uc *UseCase) Preview(ctx context.Context, req Request) (*domain.Preview, error) {
	checked, err := uc.check(ctx, req)
	if err != nil {
		return nil, toAppError("preview import", err)
	}

	return &checked.preview, nil
}

// Confirm re-checks the file and imports it as a whole. A file with any error
// is refused and nothing is written.
func (uc *UseCase) Confirm(ctx context.Context, req Request) (*domain.Result, error) {
	if err := uc.basePermission(req.Type, req.Permissions); err != nil {
		return nil, toAppError("confirm import", err)
	}

	hash := sha256.Sum256(req.Content)
	sum := hex.EncodeToString(hash[:])

	if err := uc.runs.Lock(ctx, req.GarageID, string(req.Type), sum); err != nil {
		return nil, toAppError("confirm import", err)
	}

	run, err := uc.runs.Find(ctx, req.GarageID, string(req.Type), sum)
	if err != nil {
		return nil, toAppError("confirm import", err)
	}

	if run != nil {
		return nil, toAppError("confirm import", domain.ErrFileAlreadyImported)
	}

	checked, err := uc.check(ctx, req)
	if err != nil {
		return nil, toAppError("confirm import", err)
	}

	if !checked.preview.CanConfirm {
		return nil, toAppError("confirm import", domain.ErrImportFileInvalid)
	}

	result, documentID, err := uc.apply(ctx, req, checked)
	if err != nil {
		return nil, toAppError("confirm import", err)
	}

	run = &Run{
		ID: uuid.NewString(), GarageID: req.GarageID, Type: string(req.Type), FileSHA256: sum,
		FileName: req.FileName, RowCount: checked.preview.TotalRows, CreatedBy: actorRef(req.ActorID),
	}
	if documentID != "" {
		run.DocumentID = &documentID
	}

	if err := uc.runs.Create(ctx, run); err != nil {
		return nil, toAppError("confirm import", err)
	}

	if err := uc.audit.Record(ctx, audit.Entry{
		Actor: req.ActorID, GarageID: req.GarageID, Action: ActionImportCompleted, EntityType: entityImportRun, EntityID: run.ID,
		After: map[string]any{
			"type": req.Type, "file_name": req.FileName, "file_sha256": sum, "rows": checked.preview.TotalRows,
			"counts": result.Counts, "document_id": documentID,
		},
		IP: middleware.ClientIP(ctx),
	}); err != nil {
		return nil, toAppError("confirm import", err)
	}

	return result, nil
}

// basePermission requires the permission of the import type itself.
func (uc *UseCase) basePermission(typ domain.Type, p Permissions) error {
	switch typ {
	case domain.TypeParts:
		if !p.CatalogueWrite {
			return domain.ErrPermissionRequired
		}
	case domain.TypeGoodsReceipts:
		if !p.InventoryWrite {
			return domain.ErrPermissionRequired
		}
	case domain.TypeStockCounts:
		if !p.InventoryCount {
			return domain.ErrPermissionRequired
		}
	default:
		return domain.ErrUnsupportedType
	}

	return nil
}

// ToGRPCError classifies err and translates it for the gRPC handler.
func ToGRPCError(err error) error {
	return apperror.ToGRPCError(toAppError("import", err))
}

// toAppError classifies the module's errors and passes already classified
// errors (from the Catalogue or Inventory) through.
func toAppError(op string, err error) error {
	if _, ok := apperror.As(err); ok {
		return err
	}

	switch {
	case errors.Is(err, domain.ErrPermissionRequired):
		return apperror.Wrap(apperror.CodePermissionDenied, err.Error(), err).WithReason(domain.CodePermissionRequired)
	case errors.Is(err, domain.ErrFileAlreadyImported):
		return apperror.Wrap(apperror.CodeAlreadyExists, err.Error(), err).WithReason("IMPORT_FILE_ALREADY_IMPORTED")
	case errors.Is(err, domain.ErrImportFileInvalid):
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err).WithReason("IMPORT_FILE_INVALID")
	case errors.Is(err, domain.ErrUnsupportedType):
		return apperror.New(apperror.CodeInvalidArgument, err.Error())
	default:
		return apperror.Internal(op, err)
	}
}

func actorRef(userID string) *string {
	if userID == "" {
		return nil
	}

	return &userID
}
