package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
)

func TestBarcodeRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)

	carton := caseID
	part := samplePart()
	part.Images = nil
	part.Barcodes = []domain.PartBarcode{
		{Barcode: "8930001"}, {Barcode: "8930024", UnitID: &carton, UnitName: "Carton", UnitAbbreviation: "ctn"},
	}

	e.repo.EXPECT().FindPart(anyArg, garageID, partID).Return(part, nil)
	e.repo.EXPECT().ReplaceBarcodes(anyArg, garageID, partID, actorID, anyArg).Return(nil).Once()

	set, err := e.srv.SetPartBarcodes(ctx, &cataloguev1.SetPartBarcodesRequest{
		GarageId: garageID, PartId: partID,
		Barcodes: []*cataloguev1.PartBarcode{{Barcode: "8930001"}, {Barcode: "8930024", UnitId: &carton}},
	})
	require.NoError(t, err)
	require.Len(t, set.GetPart().GetBarcodes(), 2)
	assert.Equal(t, "ctn", set.GetPart().GetBarcodes()[1].GetUnitAbbreviation())
	assert.Equal(t, caseID, set.GetPart().GetBarcodes()[1].GetUnitId())

	e.repo.EXPECT().FindBarcode(anyArg, garageID, "8930024").Return(part, &part.Barcodes[1], nil).Once()

	found, err := e.srv.LookupPartByBarcode(ctx, &cataloguev1.LookupPartByBarcodeRequest{GarageId: garageID, Barcode: "8930024"})
	require.NoError(t, err)
	assert.Equal(t, partID, found.GetPart().GetId())
	assert.Equal(t, "8930024", found.GetBarcode().GetBarcode())
	assert.Equal(t, caseID, found.GetBarcode().GetUnitId())
}

func TestBarcodeRPCFailures(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)

	e.repo.EXPECT().FindPart(anyArg, garageID, partID).Return(samplePart(), nil)
	e.repo.EXPECT().ReplaceBarcodes(anyArg, garageID, partID, actorID, anyArg).Return(domain.ErrBarcodeAlreadyUsed).Once()
	e.repo.EXPECT().FindBarcode(anyArg, garageID, "0").Return(nil, nil, domain.ErrBarcodeNotFound).Once()

	_, err := e.srv.SetPartBarcodes(ctx, &cataloguev1.SetPartBarcodesRequest{
		GarageId: garageID, PartId: partID, Barcodes: []*cataloguev1.PartBarcode{{Barcode: "dup"}},
	})
	assert.Equal(t, codes.AlreadyExists, code(err))

	_, err = e.srv.SetPartBarcodes(ctx, &cataloguev1.SetPartBarcodesRequest{
		GarageId: garageID, PartId: partID, Barcodes: []*cataloguev1.PartBarcode{{Barcode: "a_b"}},
	})
	assert.Equal(t, codes.InvalidArgument, code(err))

	_, err = e.srv.LookupPartByBarcode(ctx, &cataloguev1.LookupPartByBarcodeRequest{GarageId: garageID, Barcode: "0"})
	assert.Equal(t, codes.NotFound, code(err))

	_, err = e.srv.LookupPartByBarcode(ctx, &cataloguev1.LookupPartByBarcodeRequest{GarageId: garageID, Barcode: "a_b"})
	assert.Equal(t, codes.InvalidArgument, code(err))
}
