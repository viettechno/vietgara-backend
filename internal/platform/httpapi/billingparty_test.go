package httpapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"

	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/pkg/billingparty"
)

func TestBillingPartyToProto(t *testing.T) {
	t.Parallel()

	assert.Nil(t, httpapi.BillingPartyToProto(billingparty.Party{}), "a retail receipt has no party")

	company := httpapi.BillingPartyToProto(billingparty.Party{
		TaxProfileID: "p", Kind: "company", LegalName: "An Phat", TaxID: "0301234567", Address: "12 Le Loi", InvoiceEmail: "a@b.vn",
	})
	require.NotNil(t, company)
	assert.Equal(t, commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY, company.GetKind())
	assert.Equal(t, "0301234567", company.GetTaxId())
	assert.Equal(t, "a@b.vn", company.GetInvoiceEmail())

	personal := httpapi.BillingPartyToProto(billingparty.Party{TaxProfileID: "p", Kind: "personal"})
	assert.Equal(t, commonv1.TaxProfileKind_TAX_PROFILE_KIND_PERSONAL, personal.GetKind())

	unknown := httpapi.BillingPartyToProto(billingparty.Party{TaxProfileID: "p"})
	assert.Equal(t, commonv1.TaxProfileKind_TAX_PROFILE_KIND_UNSPECIFIED, unknown.GetKind())
}

func TestBillingTaxProfileID(t *testing.T) {
	t.Parallel()

	assert.Nil(t, httpapi.BillingTaxProfileID(nil), "none sent: the default applies")

	retail := httpapi.BillingTaxProfileID(&commonv1.BillingParty{})
	require.NotNil(t, retail)
	assert.Empty(t, *retail)

	chosen := httpapi.BillingTaxProfileID(&commonv1.BillingParty{TaxProfileId: "p"})
	require.NotNil(t, chosen)
	assert.Equal(t, "p", *chosen)
}
