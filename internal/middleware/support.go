package middleware

import (
	"context"
	"errors"
	"path"
	"strings"
	"unicode"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// ReasonSupportReadOnly is reported when a support session tries to change
// something (FR-IAM-11).
const ReasonSupportReadOnly = "SUPPORT_READ_ONLY"

const msgSupportReadOnly = "a support session is read-only"

// SupportSessions lets admin staff read a garage under a support grant
// (Release 8.2). It is asked only for a caller who is not a member of the
// garage, inside the request's transaction.
type SupportSessions interface {
	// SupportAccess returns the read-only access of userID to garageID under
	// an active grant, or access.ErrNoMembership when there is none or the
	// user no longer holds the platform permission to use it.
	SupportAccess(ctx context.Context, garageID, userID string) (*access.Access, error)
	// BeginRead records the read -- the grant, the API call and the entity it
	// names -- in the support access log and the audit log, then makes the
	// request's transaction read-only. It runs before the handler.
	BeginRead(ctx context.Context, a *access.Access, method, entityType, entityID string) error
}

// authorizeSupport admits a caller who is not a member of the garage but has
// an active support grant: only reads, only what a read-only role allows, and
// every read is logged before it runs. The subscription is not enforced: the
// owner of a suspended garage is exactly who support helps.
func authorizeSupport(
	ctx context.Context, support SupportSessions, rule access.Rule, userID, garageID, method string, req any,
) (context.Context, error) {
	user, err := support.SupportAccess(ctx, garageID, userID)
	if err != nil {
		if errors.Is(err, access.ErrNoMembership) {
			return nil, apperror.Wrap(apperror.CodePermissionDenied, access.ErrNoMembership.Error(), err).WithReason(ReasonGarageAccessDenied)
		}

		return nil, apperror.Internal("load support access", err)
	}

	if !isReadMethod(method) {
		return nil, apperror.New(apperror.CodePermissionDenied, msgSupportReadOnly).WithReason(ReasonSupportReadOnly)
	}

	if err := checkRule(user, rule); err != nil {
		return nil, err
	}

	entityType, entityID := supportEntity(method, req)
	if err := support.BeginRead(ctx, user, method, entityType, entityID); err != nil {
		return nil, apperror.Internal("record support read", err)
	}

	return access.WithAccess(ctx, user), nil
}

// supportEntity names what a read asks for: the kind from the call
// ("GetSettlement" -> "settlement", "ListRepairOrders" -> "repair_orders")
// and the id from the request, the first string field ending in _id other
// than the garage's. A list without such a field has no id.
func supportEntity(fullMethod string, req any) (entityType, entityID string) {
	name := path.Base(fullMethod)
	for _, prefix := range []string{"Get", "List", "Lookup"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok {
			name = rest

			break
		}
	}

	entityType = snakeCase(name)

	msg, ok := req.(proto.Message)
	if !ok {
		return entityType, ""
	}

	fields := msg.ProtoReflect().Descriptor().Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		if field.Kind() != protoreflect.StringKind || field.IsList() || field.IsMap() ||
			!strings.HasSuffix(string(field.Name()), "_id") || field.Name() == "garage_id" {
			continue
		}

		if value := loggable(msg.ProtoReflect().Get(field).String()); value != "" {
			return entityType, value
		}
	}

	return entityType, ""
}

func snakeCase(name string) string {
	var b strings.Builder

	for i, r := range name {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}

			r = unicode.ToLower(r)
		}

		b.WriteRune(r)
	}

	return b.String()
}

// maxLoggedID is the width of the log's entity id column.
const maxLoggedID = 100

// loggable makes a client-supplied id fit the log: valid text without NUL
// bytes, cut to the column's width.
func loggable(id string) string {
	id = strings.ReplaceAll(strings.ToValidUTF8(id, ""), "\x00", "")
	if len(id) > maxLoggedID {
		id = strings.ToValidUTF8(id[:maxLoggedID], "")
	}

	return id
}
