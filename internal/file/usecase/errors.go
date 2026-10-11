package usecase

import (
	"errors"

	"github.com/viettechno/vietgara-backend/internal/file/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/storage"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// toAppError classifies err into an *apperror.AppError; op names the
// failed operation for unexpected (internal) errors.
func toAppError(op string, err error) error {
	if _, ok := apperror.As(err); ok {
		return err
	}

	switch {
	case errors.Is(err, domain.ErrFileNotFound):
		return apperror.Wrap(apperror.CodeNotFound, err.Error(), err)
	case errors.Is(err, domain.ErrInvalidKind), errors.Is(err, domain.ErrInvalidFile), errors.Is(err, domain.ErrInvalidEntity):
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err)
	case errors.Is(err, domain.ErrUseModule):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err)
	case errors.Is(err, storage.ErrPrivateNotConfigured):
		return apperror.Wrap(apperror.CodeFailedPrecondition, "private file storage is not configured", err)
	default:
		return apperror.Wrap(apperror.CodeInternal, op, err)
	}
}
