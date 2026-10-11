package httpapi

import (
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"

	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// DeleteOutcome maps the outcome of the delete-or-deactivate rule
// (FR-CAT-07) to its wire form, for every handler that deletes.
func DeleteOutcome(o lifecycle.Outcome) commonv1.DeleteOutcome {
	switch o {
	case lifecycle.Deleted:
		return commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED
	case lifecycle.Deactivated:
		return commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED
	default:
		return commonv1.DeleteOutcome_DELETE_OUTCOME_UNSPECIFIED
	}
}
