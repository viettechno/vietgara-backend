package client

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	filev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/file/v1"
)

// This file covers the Catalogue module (Release 6.0): units of measure,
// the category tree, parts with their packaging and images, and the file
// upload part images use.

// dryRunQuery asks a delete to report its outcome without deleting.
const dryRunQuery = "?dryRun=true"

func unitPath(garageID, unitID string) string {
	return garagePath(garageID) + "/units/" + url.PathEscape(unitID)
}

func categoryPath(garageID, categoryID string) string {
	return garagePath(garageID) + "/part-categories/" + url.PathEscape(categoryID)
}

func partPath(garageID, partID string) string {
	return garagePath(garageID) + "/parts/" + url.PathEscape(partID)
}

// ---- Units -------------------------------------------------------------------

// ListUnits returns garageID's units of measure (includeInactive adds the
// deactivated ones).
func (c *Client) ListUnits(ctx context.Context, garageID string, includeInactive bool) ([]*cataloguev1.Unit, error) {
	path := garagePath(garageID) + "/units"
	if includeInactive {
		path += "?includeInactive=true"
	}

	resp, err := call[cataloguev1.ListUnitsResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// CreateUnit adds a unit of measure.
func (c *Client) CreateUnit(ctx context.Context, garageID, name, abbreviation string) (*cataloguev1.Unit, error) {
	return call[cataloguev1.Unit](ctx, c, http.MethodPost, garagePath(garageID)+"/units",
		&cataloguev1.Unit{Name: name, Abbreviation: abbreviation})
}

// UpdateUnit patches a unit (mask paths are proto field names).
func (c *Client) UpdateUnit(ctx context.Context, garageID, unitID string, unit *cataloguev1.Unit, paths ...string) (*cataloguev1.Unit, error) {
	path := unitPath(garageID, unitID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[cataloguev1.Unit](ctx, c, http.MethodPatch, path, unit)
}

// DeleteUnit deletes a unit, or deactivates it when parts or documents
// still use it; dryRun only reports which.
func (c *Client) DeleteUnit(ctx context.Context, garageID, unitID string, dryRun bool) (*cataloguev1.DeleteUnitResponse, error) {
	path := unitPath(garageID, unitID)
	if dryRun {
		path += dryRunQuery
	}

	return call[cataloguev1.DeleteUnitResponse](ctx, c, http.MethodDelete, path, nil)
}

// ---- Categories --------------------------------------------------------------

// ListPartCategories returns the category tree as a flat list.
func (c *Client) ListPartCategories(ctx context.Context, garageID string, includeInactive bool) ([]*cataloguev1.PartCategory, error) {
	path := garagePath(garageID) + "/part-categories"
	if includeInactive {
		path += "?includeInactive=true"
	}

	resp, err := call[cataloguev1.ListPartCategoriesResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// CreatePartCategory adds a category; parentID "" creates a root.
func (c *Client) CreatePartCategory(ctx context.Context, garageID, name, parentID string) (*cataloguev1.PartCategory, error) {
	category := &cataloguev1.PartCategory{Name: name}
	if parentID != "" {
		category.ParentId = &parentID
	}

	return call[cataloguev1.PartCategory](ctx, c, http.MethodPost, garagePath(garageID)+"/part-categories", category)
}

// UpdatePartCategory patches a category (mask paths: name, parent_id,
// is_active).
func (c *Client) UpdatePartCategory(
	ctx context.Context, garageID, categoryID string, category *cataloguev1.PartCategory, paths ...string,
) (*cataloguev1.PartCategory, error) {
	path := categoryPath(garageID, categoryID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[cataloguev1.PartCategory](ctx, c, http.MethodPatch, path, category)
}

// DeletePartCategory deletes a category, or deactivates it when it has
// sub-categories or parts.
func (c *Client) DeletePartCategory(ctx context.Context, garageID, categoryID string, dryRun bool) (*cataloguev1.DeletePartCategoryResponse, error) {
	path := categoryPath(garageID, categoryID)
	if dryRun {
		path += dryRunQuery
	}

	return call[cataloguev1.DeletePartCategoryResponse](ctx, c, http.MethodDelete, path, nil)
}

// ---- Parts -------------------------------------------------------------------

// ListParts returns garageID's parts filtered by query parameters (query,
// categoryId, brand, includeInactive, page, pageSize).
func (c *Client) ListParts(ctx context.Context, garageID string, query url.Values) ([]*cataloguev1.Part, error) {
	path := garagePath(garageID) + "/parts"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := call[cataloguev1.ListPartsResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// SearchParts returns the parts matching query (SKU, name or brand).
func (c *Client) SearchParts(ctx context.Context, garageID, query string) ([]*cataloguev1.Part, error) {
	return c.ListParts(ctx, garageID, url.Values{"query": {query}})
}

// CreatePart adds a part to the catalogue.
func (c *Client) CreatePart(ctx context.Context, garageID string, part *cataloguev1.Part) (*cataloguev1.Part, error) {
	return call[cataloguev1.Part](ctx, c, http.MethodPost, garagePath(garageID)+"/parts", part)
}

// GetPart returns one part with its packaging units and images.
func (c *Client) GetPart(ctx context.Context, garageID, partID string) (*cataloguev1.Part, error) {
	return call[cataloguev1.Part](ctx, c, http.MethodGet, partPath(garageID, partID), nil)
}

// UpdatePart patches a part (mask paths are proto field names).
func (c *Client) UpdatePart(ctx context.Context, garageID, partID string, part *cataloguev1.Part, paths ...string) (*cataloguev1.Part, error) {
	path := partPath(garageID, partID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[cataloguev1.Part](ctx, c, http.MethodPatch, path, part)
}

// DeletePart deletes a part, or deactivates it when stock movements or
// documents refer to it; dryRun only reports which.
func (c *Client) DeletePart(ctx context.Context, garageID, partID string, dryRun bool) (*cataloguev1.DeletePartResponse, error) {
	path := partPath(garageID, partID)
	if dryRun {
		path += dryRunQuery
	}

	return call[cataloguev1.DeletePartResponse](ctx, c, http.MethodDelete, path, nil)
}

// ActivatePart reactivates a deactivated part.
func (c *Client) ActivatePart(ctx context.Context, garageID, partID string) (*cataloguev1.Part, error) {
	return call[cataloguev1.Part](ctx, c, http.MethodPost, partPath(garageID, partID)+":activate",
		&cataloguev1.ActivatePartRequest{GarageId: garageID, PartId: partID})
}

// SetPartUnits replaces the part's packaging table.
func (c *Client) SetPartUnits(ctx context.Context, garageID, partID string, units []*cataloguev1.PackagingUnit) (*cataloguev1.Part, error) {
	return call[cataloguev1.Part](ctx, c, http.MethodPut, partPath(garageID, partID)+"/units",
		&cataloguev1.SetPartUnitsRequest{GarageId: garageID, PartId: partID, PackagingUnits: units})
}

// AddPartImage attaches an uploaded part_image file to the part.
func (c *Client) AddPartImage(ctx context.Context, garageID, partID, fileID string) (*cataloguev1.Part, error) {
	return call[cataloguev1.Part](ctx, c, http.MethodPost, partPath(garageID, partID)+"/images",
		&cataloguev1.AddPartImageRequest{GarageId: garageID, PartId: partID, FileId: fileID})
}

// RemovePartImage detaches an image from the part.
func (c *Client) RemovePartImage(ctx context.Context, garageID, partID, fileID string) (*cataloguev1.Part, error) {
	return call[cataloguev1.Part](ctx, c, http.MethodDelete, partPath(garageID, partID)+"/images/"+url.PathEscape(fileID), nil)
}

// ---- Files -------------------------------------------------------------------

// UploadPartImage uploads an image for a part; attach it with AddPartImage.
func (c *Client) UploadPartImage(ctx context.Context, garageID, partID string, content []byte, contentType string) (*filev1.File, error) {
	return call[filev1.File](ctx, c, http.MethodPost, garagePath(garageID)+"/files", &filev1.UploadFileRequest{
		GarageId: garageID, Kind: filev1.FileKind_FILE_KIND_PART_IMAGE, EntityType: "part", EntityId: partID,
		Content: content, ContentType: contentType,
	})
}

// GetFileURL returns where a file is served from.
func (c *Client) GetFileURL(ctx context.Context, garageID, fileID string) (*filev1.GetFileUrlResponse, error) {
	return call[filev1.GetFileUrlResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/files/"+url.PathEscape(fileID)+"/url", nil)
}

// DeleteFile deletes a file, or deactivates it when something still uses
// it; dryRun only reports which.
func (c *Client) DeleteFile(ctx context.Context, garageID, fileID string, dryRun bool) (*filev1.DeleteFileResponse, error) {
	path := garagePath(garageID) + "/files/" + url.PathEscape(fileID)
	if dryRun {
		path += dryRunQuery
	}

	return call[filev1.DeleteFileResponse](ctx, c, http.MethodDelete, path, nil)
}
