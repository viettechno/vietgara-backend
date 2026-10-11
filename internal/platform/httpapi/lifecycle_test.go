package httpapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"

	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

func TestDeleteOutcome(t *testing.T) {
	t.Parallel()

	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED, httpapi.DeleteOutcome(lifecycle.Deleted))
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, httpapi.DeleteOutcome(lifecycle.Deactivated))
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_UNSPECIFIED, httpapi.DeleteOutcome(""))
}
