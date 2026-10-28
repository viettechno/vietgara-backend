package render

import (
	"errors"
	"fmt"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
)

// ErrModelMismatch is returned when a template is given another document's
// model.
var ErrModelMismatch = errors.New("render: the model is not the template's document")

// Template is one built-in template: a document type on a paper size, at a
// version. A released template is never edited: a change is a new version,
// and the old one stays here for as long as a printed document may need it,
// so a reprint matches the first print.
type Template struct {
	Type    printmodel.DocumentType
	Paper   printmodel.Paper
	Version int

	render func(printmodel.Model, Options) ([]byte, error)
}

// Render prints m, with the copy mark when copyMark is set.
func (t Template) Render(m printmodel.Model, copyMark bool) ([]byte, error) {
	return t.render(m, Options{Paper: t.Paper, Copy: copyMark})
}

// entry registers fn, a template of the model type M.
func entry[M printmodel.Model](paper printmodel.Paper, version int, fn func(M, Options) ([]byte, error)) Template {
	var zero M

	return Template{
		Type: zero.Type(), Paper: paper, Version: version,
		render: func(m printmodel.Model, o Options) ([]byte, error) {
			typed, ok := m.(M)
			if !ok {
				return nil, fmt.Errorf("%w: %T for %s", ErrModelMismatch, m, zero.Type())
			}

			return fn(typed, o)
		},
	}
}

// templates is the registry. Version 1 of every A4 template is the layout the
// documents had before Release 10.0; the A5 templates exist for the
// documents a garage hands over on a slip.
var templates = []Template{
	entry(printmodel.A4, 1, quoteTemplate),
	entry(printmodel.A5, 1, quoteTemplate),
	entry(printmodel.A4, 1, repairOrderTemplate),
	entry(printmodel.A4, 1, settlementTemplate),
	entry(printmodel.A5, 1, settlementTemplate),
	entry(printmodel.A4, 1, paymentVoucherTemplate),
	entry(printmodel.A5, 1, paymentVoucherTemplate),
	entry(printmodel.A4, 1, goodsReceiptTemplate),
	entry(printmodel.A4, 1, stockIssueTemplate),
}

// Lookup returns the template of a document type, paper size and version.
func Lookup(t printmodel.DocumentType, paper printmodel.Paper, version int) (Template, bool) {
	for _, tpl := range templates {
		if tpl.Type == t && tpl.Paper == paper && tpl.Version == version {
			return tpl, true
		}
	}

	return Template{}, false
}

// Latest returns the newest version of the template of a document type and
// paper size.
func Latest(t printmodel.DocumentType, paper printmodel.Paper) (Template, bool) {
	var latest Template

	found := false

	for _, tpl := range templates {
		if tpl.Type == t && tpl.Paper == paper && (!found || tpl.Version > latest.Version) {
			latest, found = tpl, true
		}
	}

	return latest, found
}

// Papers lists the paper sizes a document type has a template for, A4 first.
func Papers(t printmodel.DocumentType) []printmodel.Paper {
	var papers []printmodel.Paper

	for _, paper := range []printmodel.Paper{printmodel.A4, printmodel.A5} {
		if _, ok := Latest(t, paper); ok {
			papers = append(papers, paper)
		}
	}

	return papers
}

// copyLabel is the copy mark to print, or "" when the print is not a copy.
func copyLabel(o Options, label string) string {
	if o.Copy {
		return label
	}

	return ""
}
