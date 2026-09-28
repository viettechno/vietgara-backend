package usecase

import (
	"errors"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// toAppError classifies err into an *apperror.AppError; op names the
// failed operation for unexpected (internal) errors.
func toAppError(op string, err error) error {
	if _, ok := apperror.As(err); ok {
		return err
	}

	type mapping struct {
		target error
		code   apperror.Code
		reason string
	}

	for _, m := range []mapping{
		{domain.ErrGarageNotFound, apperror.CodeNotFound, ""},
		{domain.ErrStaffNotFound, apperror.CodeNotFound, ""},
		{domain.ErrRoleNotFound, apperror.CodeNotFound, ""},
		{domain.ErrGroupNotFound, apperror.CodeNotFound, ""},
		{domain.ErrInvitationNotFound, apperror.CodeNotFound, ""},
		{domain.ErrAccountNotFound, apperror.CodeNotFound, domain.ReasonAccountNotFound},
		{domain.ErrAccountInactive, apperror.CodeFailedPrecondition, domain.ReasonAccountInactive},
		{domain.ErrOwnerMembership, apperror.CodeFailedPrecondition, domain.ReasonOwnerMembership},
		{domain.ErrAlreadyMember, apperror.CodeAlreadyExists, domain.ReasonAlreadyMember},
		{domain.ErrAlreadyInvited, apperror.CodeAlreadyExists, domain.ReasonAlreadyInvited},
		{domain.ErrInvitationNotPending, apperror.CodeFailedPrecondition, domain.ReasonInvitationNotPending},
		{domain.ErrInvitationExpired, apperror.CodeFailedPrecondition, domain.ReasonInvitationExpired},
		{domain.ErrRoleNameTaken, apperror.CodeAlreadyExists, domain.ReasonRoleNameTaken},
		{domain.ErrRoleInUse, apperror.CodeFailedPrecondition, domain.ReasonRoleInUse},
		{domain.ErrGroupNameTaken, apperror.CodeAlreadyExists, domain.ReasonGroupNameTaken},
		{domain.ErrInvalidLogo, apperror.CodeInvalidArgument, domain.ReasonInvalidLogo},
	} {
		if errors.Is(err, m.target) {
			appErr := apperror.Wrap(m.code, m.target.Error(), err)
			if m.reason != "" {
				appErr = appErr.WithReason(m.reason)
			}

			return appErr
		}
	}

	return apperror.Internal(op, err)
}
