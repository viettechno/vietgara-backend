package usecase

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// CategoryFields are the settable fields of a category. ParentID is a
// pointer to a pointer-like value: Set says whether it is given at all, so a
// move to the root (empty ParentID) can be told from "unchanged".
type CategoryFields struct {
	Name      *string
	SetParent bool
	ParentID  string
	IsActive  *bool
}

// CreateCategoryRequest is the input for UseCase.CreateCategory.
type CreateCategoryRequest struct {
	GarageID string
	ActorID  string
	Fields   CategoryFields
}

// UpdateCategoryRequest is the input for UseCase.UpdateCategory.
type UpdateCategoryRequest struct {
	GarageID   string
	CategoryID string
	ActorID    string
	Fields     CategoryFields
}

// CreateCategory adds a category at most three levels deep (FR-CAT-03).
func (uc *UseCase) CreateCategory(ctx context.Context, req CreateCategoryRequest) (*domain.Category, error) {
	c := &domain.Category{
		ID: uuid.NewString(), GarageID: req.GarageID, IsActive: true,
		CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}

	if req.Fields.Name == nil {
		return nil, toAppError("create category", domain.ErrCategoryNameRequired)
	}

	all, err := uc.repo.ListCategories(ctx, req.GarageID, true)
	if err != nil {
		return nil, toAppError("create category", err)
	}

	if err := applyCategoryFields(c, req.Fields, all); err != nil {
		return nil, toAppError("create category", err)
	}

	if err := uc.repo.CreateCategory(ctx, c); err != nil {
		return nil, toAppError("create category", err)
	}

	return c, nil
}

// UpdateCategory renames, moves or (de)activates a category. A move keeps the
// whole subtree within three levels and never goes under the category itself.
func (uc *UseCase) UpdateCategory(ctx context.Context, req UpdateCategoryRequest) (*domain.Category, error) {
	c, err := uc.repo.FindCategory(ctx, req.GarageID, req.CategoryID)
	if err != nil {
		return nil, toAppError("update category", err)
	}

	all, err := uc.repo.ListCategories(ctx, req.GarageID, true)
	if err != nil {
		return nil, toAppError("update category", err)
	}

	if err := applyCategoryFields(c, req.Fields, all); err != nil {
		return nil, toAppError("update category", err)
	}

	c.UpdatedBy = actorRef(req.ActorID)

	if err := uc.repo.UpdateCategory(ctx, c); err != nil {
		return nil, toAppError("update category", err)
	}

	return c, nil
}

// ListCategories returns the whole tree as a flat list ordered by name.
func (uc *UseCase) ListCategories(ctx context.Context, garageID string, includeInactive bool) ([]*domain.Category, error) {
	categories, err := uc.repo.ListCategories(ctx, garageID, includeInactive)
	if err != nil {
		return nil, toAppError("list categories", err)
	}

	return categories, nil
}

// DeleteCategory follows the delete-or-deactivate rule (FR-CAT-07): a category
// with sub-categories or parts is deactivated, never deleted from under them.
func (uc *UseCase) DeleteCategory(ctx context.Context, req DeleteRequest) (lifecycle.Outcome, error) {
	if _, err := uc.repo.FindCategory(ctx, req.GarageID, req.ID); err != nil {
		return "", toAppError("delete category", err)
	}

	outcome, err := lifecycle.Apply(ctx, req.DryRun, lifecycle.Rule{
		InUse: func(ctx context.Context) (bool, error) {
			all, err := uc.repo.ListCategories(ctx, req.GarageID, true)
			if err != nil {
				return false, err
			}

			if len(childrenOf(all, req.ID)) > 0 {
				return true, nil
			}

			return uc.repo.CategoryHasParts(ctx, req.GarageID, req.ID)
		},
		Delete: func(ctx context.Context) error {
			return uc.repo.SoftDeleteCategory(ctx, req.GarageID, req.ID, req.ActorID)
		},
		Deactivate: func(ctx context.Context) error {
			return uc.repo.SetCategoryActive(ctx, req.GarageID, req.ID, false, req.ActorID)
		},
	})
	if err != nil {
		return "", toAppError("delete category", err)
	}

	return outcome, nil
}

func applyCategoryFields(c *domain.Category, f CategoryFields, all []*domain.Category) error {
	if f.Name != nil {
		name := strings.TrimSpace(*f.Name)
		if name == "" {
			return domain.ErrCategoryNameRequired
		}

		if err := domain.CheckLength(name, domain.MaxCategoryNameLen); err != nil {
			return err
		}

		c.Name = name
	}

	if f.SetParent {
		if err := moveCategory(c, f.ParentID, all); err != nil {
			return err
		}
	}

	if f.IsActive != nil {
		c.IsActive = *f.IsActive
	}

	return nil
}

// moveCategory points c at parentID ("" is the root) after checking the
// parent exists, that c is not moved under itself or its own subtree, and
// that c's subtree still fits in three levels.
func moveCategory(c *domain.Category, parentID string, all []*domain.Category) error {
	if parentID == "" {
		c.ParentID = nil

		return nil
	}

	byID := make(map[string]*domain.Category, len(all))
	for _, x := range all {
		byID[x.ID] = x
	}

	parent, ok := byID[parentID]
	if !ok {
		return domain.ErrCategoryNotFound
	}

	if !parent.IsActive {
		return domain.ErrInactiveCategory
	}

	// Depth of the parent chain (the parent's own level counts).
	parentLevel := 1

	for cur := parent; cur.ParentID != nil; parentLevel++ {
		if *cur.ParentID == c.ID || cur.ID == c.ID {
			return domain.ErrCategoryCycle
		}

		next, ok := byID[*cur.ParentID]
		if !ok {
			break
		}

		cur = next
	}

	if parent.ID == c.ID {
		return domain.ErrCategoryCycle
	}

	if parentLevel+1+subtreeHeight(all, c.ID) > domain.MaxCategoryDepth {
		return domain.ErrCategoryDepth
	}

	c.ParentID = &parentID

	return nil
}

// subtreeHeight is the number of levels below id (0 for a leaf).
func subtreeHeight(all []*domain.Category, id string) int {
	height := 0

	for _, child := range childrenOf(all, id) {
		height = max(height, 1+subtreeHeight(all, child.ID))
	}

	return height
}

func childrenOf(all []*domain.Category, id string) []*domain.Category {
	var out []*domain.Category

	for _, c := range all {
		if c.ParentID != nil && *c.ParentID == id {
			out = append(out, c)
		}
	}

	return out
}

// descendantIDs returns id and every category below it.
func descendantIDs(all []*domain.Category, id string) []string {
	out := []string{id}

	for _, child := range childrenOf(all, id) {
		out = append(out, descendantIDs(all, child.ID)...)
	}

	return out
}
