package usecase

import (
	"context"
	"math/big"
	"strconv"
	"strings"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
	"github.com/viettechno/vietgara-backend/internal/dataimport/template"
)

// checkedFile is a validated workbook: the answer of Preview and, when
// CanConfirm, the rows Confirm applies.
type checkedFile struct {
	preview  domain.Preview
	parts    []partRow
	receipts []receiptRow
	counts   []countRow
	// newUnits and newCategories are the names a parts import must create.
	newUnits      []string
	newCategories []string
	// The document-level columns of a receipt or count file: the first
	// non-empty value of each, which every other row must repeat.
	receiptDate, deliveryNote, receiptNotes string
	countName, countNotes                   string
}

type partRow struct {
	row          int
	sku          string
	name         string
	brand        string
	baseUnit     string
	baseUnitAbbr string
	category     string
	barcodes     []barcodeInput
	packaging    []packInput
	openingQty   string
	unitCost     int64
	supplierName string
	supplierID   string
}

// barcodeInput is one code of a part row; unit is the packaging unit it
// stands for, empty for the base unit.
type barcodeInput struct {
	code string
	unit string
}

type packInput struct {
	name   string
	factor string
}

type receiptRow struct {
	row          int
	receiptDate  string
	deliveryNote string
	notes        string
	sku          string
	partID       string
	quantity     string
	unitID       string
	unitCost     int64
	supplierName string
	supplierID   string
}

type countRow struct {
	row      int
	name     string
	notes    string
	sku      string
	partID   string
	quantity string
	unitID   string
}

// table is a workbook sheet mapped by the expected headers.
type table struct {
	headers []string
	index   map[string]int
	rows    []SheetRow
	total   int
	tooMany bool
}

type refs struct {
	parts       map[string]PartInfo
	units       map[string]UnitInfo
	unitsByAbbr map[string]UnitInfo
	categories  map[string]CategoryInfo
	suppliers   map[string]string
	barcodes    map[string]string
	byBarcode   map[string]BarcodeMatch
}

// sink collects row errors, capped at domain.MaxErrors.
type sink struct {
	preview   *domain.Preview
	bad       map[int]bool
	truncated bool
}

func (s *sink) add(row int, column, code, message string) {
	if row > 0 {
		s.bad[row] = true
	}

	if len(s.preview.Errors) >= domain.MaxErrors {
		s.truncated = true

		return
	}

	s.preview.Errors = append(s.preview.Errors, domain.RowError{Row: row, Column: column, Code: code, Message: message})
}

var requiredColumns = map[domain.Type][]string{
	domain.TypeParts:         {template.ColSKU, template.ColName, template.ColBaseUnit, template.ColBaseUnitAbbr},
	domain.TypeGoodsReceipts: {template.ColSKU, template.ColQuantity, template.ColSupplier},
	domain.TypeStockCounts:   {template.ColSKU, template.ColCountedQuantity},
}

func (uc *UseCase) check(ctx context.Context, req Request) (*checkedFile, error) {
	if _, ok := domain.ParseType(string(req.Type)); !ok {
		return nil, domain.ErrUnsupportedType
	}

	checked := &checkedFile{preview: domain.Preview{
		Type: req.Type, Columns: template.Columns(req.Type), Counts: map[string]int{},
		Errors: []domain.RowError{}, SampleRows: []domain.SampleRow{},
	}}
	s := &sink{preview: &checked.preview, bad: map[int]bool{}}

	if err := uc.basePermission(req.Type, req.Permissions); err != nil {
		s.add(0, "", domain.CodePermissionRequired, permissionMessage(req.Type))
	}

	t, ok := uc.readTable(checked, req, s)
	if !ok {
		return checked, nil
	}

	uc.checkColumnPermissions(t, req, s)

	if len(checked.preview.Errors) > 0 && onlyFileErrors(checked.preview.Errors) {
		checked.preview.ValidRows = 0
		return checked, nil
	}

	reference, err := uc.loadReferences(ctx, req.GarageID, req.Type, t)
	if err != nil {
		return nil, err
	}

	switch req.Type {
	case domain.TypeParts:
		uc.validateParts(t, req.Permissions, reference, s, checked)
	case domain.TypeGoodsReceipts:
		uc.validateReceipts(t, req.Permissions, reference, s, checked)
	case domain.TypeStockCounts:
		uc.validateCounts(t, reference, s, checked)
	}

	finish(checked, t, s)

	return checked, nil
}

// readTable reads the workbook and checks what can be checked before any row:
// size, readability, headers and the number of rows. ok is false when the
// file cannot be looked at further; the reason is already in the preview.
func (uc *UseCase) readTable(checked *checkedFile, req Request, s *sink) (*table, bool) {
	switch {
	case len(req.Content) == 0:
		s.add(0, "", domain.CodeFileEmpty, "the file is empty")
		return nil, false
	case len(req.Content) > domain.MaxFileBytes:
		s.add(0, "", domain.CodeFileTooLarge, "the file is larger than 5 MB")
		return nil, false
	}

	sheet, err := uc.reader.Read(req.Content)
	if err != nil {
		// An unreadable file is a finding of the preview, not a failure of it.
		s.add(0, "", domain.CodeInvalidFile, "the file is not a readable .xlsx workbook")
		return nil, false
	}

	t, ok := uc.buildTable(sheet, req.Type, s)
	if !ok {
		return nil, false
	}

	checked.preview.TotalRows = t.total
	checked.preview.SampleRows = sampleRows(t)

	if t.tooMany {
		s.add(0, "", domain.CodeTooManyRows, "the file has more than 5000 data rows")
		return nil, false
	}

	return t, true
}

// finish totals the valid rows and decides whether the file can be confirmed.
func finish(checked *checkedFile, t *table, s *sink) {
	checked.preview.ErrorsTruncated = s.truncated
	checked.preview.ValidRows = max(len(t.rows)-len(s.bad), 0)
	checked.preview.CanConfirm = checked.preview.TotalRows > 0 && checked.preview.ValidRows == checked.preview.TotalRows &&
		len(checked.preview.Errors) == 0 && !s.truncated
}

func (uc *UseCase) buildTable(sheet *Sheet, typ domain.Type, s *sink) (*table, bool) {
	headers := template.Columns(typ)
	t := &table{headers: headers, index: map[string]int{}}

	if sheet == nil || len(sheet.Rows) == 0 {
		s.add(0, "", domain.CodeFileEmpty, "the sheet has no header row")

		return nil, false
	}

	header := sheet.Rows[0]
	for i, cell := range header.Cells {
		key := domain.NormalizeHeader(cell)
		if key == "" {
			continue
		}

		if _, seen := t.index[key]; !seen {
			t.index[key] = i
		}
	}

	t.rows = sheet.Rows[1:]
	t.total = len(t.rows)

	if len(t.rows) > domain.MaxRows {
		t.tooMany = true
		t.rows = t.rows[:domain.MaxRows]
	}

	missing := false

	for _, required := range requiredColumns[typ] {
		if _, ok := t.index[domain.NormalizeHeader(required)]; !ok {
			s.add(0, required, domain.CodeMissingColumn, "the template column "+required+" is missing")

			missing = true
		}
	}

	return t, !missing
}

func (t *table) cell(row SheetRow, header string) string {
	i, ok := t.index[domain.NormalizeHeader(header)]
	if !ok || i >= len(row.Cells) {
		return ""
	}

	return strings.TrimSpace(row.Cells[i])
}

func (t *table) hasValue(header string) bool {
	for _, row := range t.rows {
		if t.cell(row, header) != "" {
			return true
		}
	}

	return false
}

func sampleRows(t *table) []domain.SampleRow {
	n := min(len(t.rows), domain.MaxSampleRows)
	out := make([]domain.SampleRow, 0, n)

	for _, row := range t.rows[:n] {
		cells := make([]string, 0, len(t.headers))
		for _, header := range t.headers {
			cells = append(cells, t.cell(row, header))
		}

		out = append(out, domain.SampleRow{Row: row.Number, Cells: cells})
	}

	return out
}

// checkColumnPermissions reports a row-0 error when the file uses a column
// whose permission the caller lacks (opening stock, cost).
func (uc *UseCase) checkColumnPermissions(t *table, req Request, s *sink) {
	switch req.Type {
	case domain.TypeParts:
		if t.hasValue(template.ColOpeningQuantity) && !req.Permissions.InventoryWrite {
			s.add(0, template.ColOpeningQuantity, domain.CodePermissionRequired, "opening stock needs the inventory write permission")
		}

		if t.hasValue(template.ColUnitCost) && !req.Permissions.InventoryCostRead {
			s.add(0, template.ColUnitCost, domain.CodePermissionRequired, "unit cost needs the inventory cost permission")
		}
	case domain.TypeGoodsReceipts:
		if t.hasValue(template.ColUnitCost) && !req.Permissions.InventoryCostRead {
			s.add(0, template.ColUnitCost, domain.CodePermissionRequired, "unit cost needs the inventory cost permission")
		}
	case domain.TypeStockCounts:
		// A count has no column that needs more than the count permission.
	}
}

func onlyFileErrors(errors []domain.RowError) bool {
	for _, e := range errors {
		if e.Row != 0 {
			return false
		}
	}

	return true
}

func permissionMessage(typ domain.Type) string {
	switch typ {
	case domain.TypeParts:
		return "importing parts needs the catalogue write permission"
	case domain.TypeGoodsReceipts:
		return "importing goods receipts needs the inventory write permission"
	case domain.TypeStockCounts:
		return "importing stock counts needs the inventory count permission"
	default:
		return "your role does not allow this import"
	}
}

// loadReferences resolves the existing parts, units, categories, suppliers
// and barcodes the validation compares the file with.
func (uc *UseCase) loadReferences(ctx context.Context, garageID string, typ domain.Type, t *table) (*refs, error) {
	r := &refs{
		parts: map[string]PartInfo{}, units: map[string]UnitInfo{}, unitsByAbbr: map[string]UnitInfo{},
		categories: map[string]CategoryInfo{}, suppliers: map[string]string{}, barcodes: map[string]string{},
		byBarcode: map[string]BarcodeMatch{},
	}

	skus := make([]string, 0, len(t.rows))
	for _, row := range t.rows {
		skus = append(skus, t.cell(row, template.ColSKU))
	}

	if err := uc.loadPartsAndUnits(ctx, garageID, skus, r); err != nil {
		return nil, err
	}

	if typ == domain.TypeParts {
		return r, uc.loadForParts(ctx, garageID, t, r)
	}

	return r, uc.loadForDocuments(ctx, garageID, skus, t, r)
}

// loadPartsAndUnits loads the parts the file names by SKU and every unit.
func (uc *UseCase) loadPartsAndUnits(ctx context.Context, garageID string, skus []string, r *refs) error {
	parts, err := uc.catalogue.PartsBySKUs(ctx, garageID, skus)
	if err != nil {
		return err
	}

	for _, p := range parts {
		r.parts[strings.ToLower(p.SKU)] = p
	}

	units, err := uc.catalogue.Units(ctx, garageID)
	if err != nil {
		return err
	}

	for _, u := range units {
		r.units[strings.ToLower(u.Name)] = u
		if _, ok := r.unitsByAbbr[strings.ToLower(u.Abbreviation)]; !ok {
			r.unitsByAbbr[strings.ToLower(u.Abbreviation)] = u
		}
	}

	return nil
}

// loadForDocuments resolves what a receipt or count file refers to: a code
// that is not a SKU may be a barcode, and receipts name suppliers.
func (uc *UseCase) loadForDocuments(ctx context.Context, garageID string, skus []string, t *table, r *refs) error {
	var unresolved []string

	for _, sku := range skus {
		if _, isSKU := r.parts[strings.ToLower(sku)]; !isSKU && sku != "" {
			unresolved = append(unresolved, sku)
		}
	}

	if len(unresolved) > 0 {
		matches, err := uc.catalogue.PartsByBarcodes(ctx, garageID, unresolved)
		if err != nil {
			return err
		}

		r.byBarcode = matches
	}

	names := make([]string, 0, len(t.rows))

	for _, row := range t.rows {
		if supplier := t.cell(row, template.ColSupplier); supplier != "" {
			names = append(names, supplier)
		}
	}

	return uc.loadSuppliers(ctx, garageID, names, r)
}

// loadForParts resolves what a parts file refers to: the category tree, the
// barcodes already in use and the suppliers of the opening stock.
func (uc *UseCase) loadForParts(ctx context.Context, garageID string, t *table, r *refs) error {
	categories, err := uc.catalogue.Categories(ctx, garageID)
	if err != nil {
		return err
	}

	for _, c := range categories {
		r.categories[strings.ToLower(c.Name)] = c
	}

	codes := make([]string, 0, len(t.rows))
	names := make([]string, 0, len(t.rows))

	for _, row := range t.rows {
		for _, item := range splitList(t.cell(row, template.ColBarcodes)) {
			code, _, _ := strings.Cut(item, "@")
			codes = append(codes, strings.TrimSpace(code))
		}

		if t.cell(row, template.ColOpeningQuantity) != "" && t.cell(row, template.ColSupplier) != "" {
			names = append(names, t.cell(row, template.ColSupplier))
		}
	}

	if len(codes) > 0 {
		existing, err := uc.catalogue.ExistingBarcodes(ctx, garageID, codes)
		if err != nil {
			return err
		}

		r.barcodes = existing
	}

	return uc.loadSuppliers(ctx, garageID, names, r)
}

func (uc *UseCase) loadSuppliers(ctx context.Context, garageID string, names []string, r *refs) error {
	if len(names) == 0 {
		return nil
	}

	ids, err := uc.suppliers.IDsByName(ctx, garageID, names)
	if err != nil {
		return err
	}

	for name, id := range ids {
		r.suppliers[strings.ToLower(name)] = id
	}

	return nil
}

func (uc *UseCase) validateParts(t *table, permissions Permissions, r *refs, s *sink, checked *checkedFile) {
	seenSKU := map[string]struct{}{}
	seenBarcode := map[string]struct{}{}
	newUnits := map[string]struct{}{}
	newCategories := map[string]struct{}{}

	for _, row := range t.rows {
		p, ok := validatePartRow(t, row, permissions, r, seenSKU, seenBarcode, s)
		if !ok {
			continue
		}

		checked.parts = append(checked.parts, p)

		if _, exists := r.units[strings.ToLower(p.baseUnit)]; !exists {
			newUnits[strings.ToLower(p.baseUnit)] = struct{}{}
		}

		for _, pack := range p.packaging {
			if _, exists := r.units[strings.ToLower(pack.name)]; !exists {
				newUnits[strings.ToLower(pack.name)] = struct{}{}
			}
		}

		if p.category != "" {
			if _, exists := r.categories[strings.ToLower(p.category)]; !exists {
				newCategories[strings.ToLower(p.category)] = struct{}{}
			}
		}
	}

	for _, p := range checked.parts {
		checked.preview.Counts["parts"]++
		checked.preview.Counts["barcodes"] += len(p.barcodes)

		if p.openingQty != "" && p.openingQty != "0" {
			checked.preview.Counts["receipt_lines"]++
		}
	}

	checked.preview.Counts["units"] = len(newUnits)
	checked.preview.Counts["categories"] = len(newCategories)

	for name := range newUnits {
		checked.newUnits = append(checked.newUnits, name)
	}

	for name := range newCategories {
		checked.newCategories = append(checked.newCategories, name)
	}
}

// partCheck validates one row of a parts file, collecting its errors.
type partCheck struct {
	t     *table
	row   SheetRow
	r     *refs
	s     *sink
	valid bool
	p     partRow
}

func (c *partCheck) fail(column, code, message string) {
	c.valid = false

	c.s.add(c.row.Number, column, code, message)
}

func validatePartRow(
	t *table, row SheetRow, permissions Permissions, r *refs,
	seenSKU, seenBarcode map[string]struct{}, s *sink,
) (partRow, bool) {
	c := &partCheck{t: t, row: row, r: r, s: s, valid: true, p: partRow{row: row.Number}}

	c.sku(seenSKU)
	c.text()

	packNames := c.packaging()

	c.barcodes(packNames, seenBarcode)
	c.opening(permissions)

	return c.p, c.valid
}

func (c *partCheck) sku(seen map[string]struct{}) {
	c.p.sku = c.t.cell(c.row, template.ColSKU)

	switch {
	case c.p.sku == "":
		c.fail(template.ColSKU, domain.CodeRequiredValue, "SKU is required")
	case len([]rune(c.p.sku)) > 50:
		c.fail(template.ColSKU, domain.CodeInvalidValue, "SKU must be at most 50 characters")
	default:
		key := strings.ToLower(c.p.sku)
		if _, dup := seen[key]; dup {
			c.fail(template.ColSKU, domain.CodeSKUDuplicate, "another row of this file uses the same SKU")
		}

		seen[key] = struct{}{}

		if _, exists := c.r.parts[key]; exists {
			c.fail(template.ColSKU, domain.CodeSKUAlreadyExists, "another part already uses this SKU")
		}
	}
}

// text checks the name, brand, units and category columns.
func (c *partCheck) text() {
	p := &c.p
	p.name = c.t.cell(c.row, template.ColName)
	p.brand = c.t.cell(c.row, template.ColBrand)
	p.baseUnit = c.t.cell(c.row, template.ColBaseUnit)
	p.baseUnitAbbr = c.t.cell(c.row, template.ColBaseUnitAbbr)
	p.category = c.t.cell(c.row, template.ColCategory)

	switch {
	case p.name == "":
		c.fail(template.ColName, domain.CodeRequiredValue, "name is required")
	case len([]rune(p.name)) > 255:
		c.fail(template.ColName, domain.CodeInvalidValue, "name must be at most 255 characters")
	}

	if len([]rune(p.brand)) > 100 {
		c.fail(template.ColBrand, domain.CodeInvalidValue, "brand must be at most 100 characters")
	}

	switch {
	case p.baseUnit == "":
		c.fail(template.ColBaseUnit, domain.CodeRequiredValue, "base unit is required")
	case !validateUnitName(p.baseUnit):
		c.fail(template.ColBaseUnit, domain.CodeInvalidUnit, "base unit must be at most 50 characters")
	}

	switch {
	case p.baseUnitAbbr == "":
		c.fail(template.ColBaseUnitAbbr, domain.CodeRequiredValue, "base unit abbreviation is required")
	case len([]rune(p.baseUnitAbbr)) > 20:
		c.fail(template.ColBaseUnitAbbr, domain.CodeInvalidUnit, "base unit abbreviation must be at most 20 characters")
	}

	if len([]rune(p.category)) > 255 {
		c.fail(template.ColCategory, domain.CodeInvalidCategory, "category must be at most 255 characters")
	}
}

// packaging reads the Name=Factor items and returns the lower-cased names it
// accepted, which the barcodes may refer to.
func (c *partCheck) packaging() map[string]struct{} {
	names := map[string]struct{}{}

	for _, item := range splitList(c.t.cell(c.row, template.ColPackagingUnits)) {
		pack, ok := c.packagingItem(item, names)
		if !ok {
			continue
		}

		names[strings.ToLower(pack.name)] = struct{}{}
		c.p.packaging = append(c.p.packaging, pack)
	}

	return names
}

func (c *partCheck) packagingItem(item string, seen map[string]struct{}) (packInput, bool) {
	name, factor, ok := strings.Cut(item, "=")
	name, factor = strings.TrimSpace(name), strings.TrimSpace(factor)

	switch {
	case !ok || !validateUnitName(name) || name == "":
		c.fail(template.ColPackagingUnits, domain.CodeInvalidPackaging, "packaging unit "+item+" must be Name=Factor")
		return packInput{}, false
	case strings.EqualFold(name, c.p.baseUnit):
		c.fail(template.ColPackagingUnits, domain.CodeInvalidPackaging, "the base unit cannot also be a packaging unit")
		return packInput{}, false
	}

	if _, dup := seen[strings.ToLower(name)]; dup {
		c.fail(template.ColPackagingUnits, domain.CodeInvalidPackaging, "unit "+name+" appears twice")
		return packInput{}, false
	}

	normalized, ok := parseFactor(factor)
	if !ok {
		c.fail(template.ColPackagingUnits, domain.CodeInvalidPackaging,
			"the factor of "+name+" must be a number above zero with at most 6 decimals")

		return packInput{}, false
	}

	if _, ok := unitAbbreviation(name); !ok {
		c.fail(template.ColPackagingUnits, domain.CodeInvalidUnit,
			"packaging unit "+name+" is longer than 20 characters; add it under Units first")

		return packInput{}, false
	}

	return packInput{name: name, factor: normalized}, true
}

// barcodes reads the Code and Code@Unit items: well-formed, for a packaging
// unit of this row, and used by no other row and no other part.
func (c *partCheck) barcodes(packNames, seen map[string]struct{}) {
	for _, item := range splitList(c.t.cell(c.row, template.ColBarcodes)) {
		code, unit, forUnit := strings.Cut(item, "@")
		code, unit = strings.TrimSpace(code), strings.TrimSpace(unit)

		if !validBarcode(code) {
			c.fail(template.ColBarcodes, domain.CodeBarcodeInvalid, "barcode "+code+" is invalid")
			continue
		}

		if _, known := packNames[strings.ToLower(unit)]; forUnit && !known {
			c.fail(template.ColBarcodes, domain.CodeBarcodeInvalid, "barcode "+code+": "+unit+" is not a packaging unit of this row")
			continue
		}

		key := strings.ToLower(code)
		if _, dup := seen[key]; dup {
			c.fail(template.ColBarcodes, domain.CodeBarcodeAlreadyUsed, "barcode "+code+" is already used in this file")
			continue
		}

		seen[key] = struct{}{}

		if _, used := c.r.barcodes[key]; used {
			c.fail(template.ColBarcodes, domain.CodeBarcodeAlreadyUsed, "barcode "+code+" is already used by another part")
			continue
		}

		c.p.barcodes = append(c.p.barcodes, barcodeInput{code: code, unit: unit})
	}
}

// opening checks the opening quantity, its supplier and its cost. A caller
// without the inventory write permission has no opening stock at all.
func (c *partCheck) opening(permissions Permissions) {
	p := &c.p

	p.openingQty = c.t.cell(c.row, template.ColOpeningQuantity)
	if p.openingQty == "" || !permissions.InventoryWrite {
		p.openingQty = ""

		return
	}

	if normalized, ok := parseQuantity(p.openingQty, false); ok {
		p.openingQty = normalized
	} else {
		c.fail(template.ColOpeningQuantity, domain.CodeInvalidQuantity, "opening quantity must be zero or a number with at most 3 decimals")
	}

	if p.openingQty != "0" {
		c.supplier()
	}

	if permissions.InventoryCostRead {
		c.cost()
	}
}

func (c *partCheck) supplier() {
	c.p.supplierName = c.t.cell(c.row, template.ColSupplier)

	if c.p.supplierName == "" {
		c.fail(template.ColSupplier, domain.CodeSupplierRequired, "a supplier is required for an opening quantity")
		return
	}

	id, found := c.r.suppliers[strings.ToLower(c.p.supplierName)]
	if !found {
		c.fail(template.ColSupplier, domain.CodeUnknownSupplier, "supplier "+c.p.supplierName+" does not exist")
		return
	}

	c.p.supplierID = id
}

func (c *partCheck) cost() {
	cost, ok := parseCost(c.t.cell(c.row, template.ColUnitCost))

	switch {
	case !ok:
		c.fail(template.ColUnitCost, domain.CodeInvalidCost, "unit cost must be a whole VND amount")
	case cost > 0 && c.p.openingQty == "0":
		c.fail(template.ColUnitCost, domain.CodeInvalidCost, "unit cost needs an opening quantity")
	default:
		c.p.unitCost = cost
	}
}

func (uc *UseCase) validateReceipts(t *table, permissions Permissions, r *refs, s *sink, checked *checkedFile) {
	header := newHeaderCheck(s)

	for _, row := range t.rows {
		receipt, ok := validateReceiptRow(t, row, permissions, r, s)

		header.note(row.Number, template.ColReceiptDate, receipt.receiptDate)
		header.note(row.Number, template.ColDeliveryNote, receipt.deliveryNote)
		header.note(row.Number, template.ColNotes, receipt.notes)

		if ok {
			checked.receipts = append(checked.receipts, receipt)
		}
	}

	checked.receiptDate = header.first[template.ColReceiptDate]
	checked.deliveryNote = header.first[template.ColDeliveryNote]
	checked.receiptNotes = header.first[template.ColNotes]

	checked.preview.Counts["lines"] = len(checked.receipts)
}

func validateReceiptRow(t *table, row SheetRow, permissions Permissions, r *refs, s *sink) (receiptRow, bool) {
	out := receiptRow{row: row.Number}
	valid := true

	fail := func(column, code, message string) {
		valid = false

		s.add(row.Number, column, code, message)
	}

	date, ok := parseDate(t.cell(row, template.ColReceiptDate))
	if !ok {
		fail(template.ColReceiptDate, domain.CodeInvalidDate, "receipt date must be YYYY-MM-DD or DD/MM/YYYY")
	}

	out.receiptDate = date
	out.deliveryNote = t.cell(row, template.ColDeliveryNote)
	out.notes = t.cell(row, template.ColNotes)

	part, scanned, ok := partOfRow(t, row, r, fail, template.ColSKU)
	if ok {
		out.sku, out.partID = part.SKU, part.ID
		out.unitID = resolveUnit(t, row, part, scanned, r, fail)
	}

	out.quantity = t.cell(row, template.ColQuantity)
	if normalized, ok := parseQuantity(out.quantity, true); !ok {
		fail(template.ColQuantity, domain.CodeInvalidQuantity, "quantity must be above zero with at most 3 decimals")
	} else {
		out.quantity = normalized
	}

	out.supplierName = t.cell(row, template.ColSupplier)
	switch out.supplierName {
	case "":
		fail(template.ColSupplier, domain.CodeSupplierRequired, "a supplier is required")
	default:
		id, found := r.suppliers[strings.ToLower(out.supplierName)]
		if !found {
			fail(template.ColSupplier, domain.CodeUnknownSupplier, "supplier "+out.supplierName+" does not exist")
		} else {
			out.supplierID = id
		}
	}

	if permissions.InventoryCostRead {
		cost, ok := parseCost(t.cell(row, template.ColUnitCost))
		if !ok {
			fail(template.ColUnitCost, domain.CodeInvalidCost, "unit cost must be a whole VND amount")
		} else {
			out.unitCost = cost
		}
	}

	return out, valid
}

func (uc *UseCase) validateCounts(t *table, r *refs, s *sink, checked *checkedFile) {
	seen := map[string]struct{}{}
	header := newHeaderCheck(s)

	for _, row := range t.rows {
		count, ok := validateCountRow(t, row, r, seen, s)

		header.note(row.Number, template.ColName, count.name)
		header.note(row.Number, template.ColNotes, count.notes)

		if ok {
			checked.counts = append(checked.counts, count)
		}
	}

	checked.countName = header.first[template.ColName]
	checked.countNotes = header.first[template.ColNotes]

	checked.preview.Counts["lines"] = len(checked.counts)
}

func validateCountRow(t *table, row SheetRow, r *refs, seen map[string]struct{}, s *sink) (countRow, bool) {
	out := countRow{row: row.Number, name: t.cell(row, template.ColName), notes: t.cell(row, template.ColNotes)}
	valid := true

	fail := func(column, code, message string) {
		valid = false

		s.add(row.Number, column, code, message)
	}

	part, scanned, ok := partOfRow(t, row, r, fail, template.ColSKU)
	if ok {
		key := strings.ToLower(part.SKU)
		if _, dup := seen[key]; dup {
			fail(template.ColSKU, domain.CodeSKUDuplicate, "the same part appears twice in the count")
		}

		seen[key] = struct{}{}
		out.sku, out.partID = part.SKU, part.ID
		out.unitID = resolveUnit(t, row, part, scanned, r, fail)
	}

	out.quantity = t.cell(row, template.ColCountedQuantity)
	if normalized, ok := parseQuantity(out.quantity, false); !ok {
		fail(template.ColCountedQuantity, domain.CodeInvalidQuantity, "counted quantity must be zero or a number with at most 3 decimals")
	} else {
		out.quantity = normalized
	}

	return out, valid
}

// partOfRow resolves the code of a row to an existing, active part: its SKU,
// else one of its barcodes. scanned is the packaging unit the barcode stands
// for, empty for a SKU or a base-unit barcode.
func partOfRow(
	t *table, row SheetRow, r *refs, fail func(column, code, message string), column string,
) (part PartInfo, scanned string, ok bool) {
	code := t.cell(row, column)
	if code == "" {
		fail(column, domain.CodeRequiredValue, "SKU or barcode is required")
		return PartInfo{}, "", false
	}

	key := strings.ToLower(code)

	part, ok = r.parts[key]
	if !ok {
		match, found := r.byBarcode[key]
		if !found {
			fail(column, domain.CodeUnknownSKU, "no part has the SKU or barcode "+code)
			return PartInfo{}, "", false
		}

		part, scanned = match.Part, match.UnitID
	}

	if !part.IsActive {
		fail(column, domain.CodePartInactive, "part "+part.SKU+" is not active")
		return PartInfo{}, "", false
	}

	return part, scanned, true
}

// resolveUnit resolves the optional unit column to a unit of the part; empty
// means the base unit. Without the column, the unit of a scanned barcode
// applies; with both, they must agree.
func resolveUnit(t *table, row SheetRow, part PartInfo, scanned string, r *refs, fail func(column, code, message string)) string {
	value := t.cell(row, template.ColUnit)
	if value == "" {
		return scanned
	}

	unit, ok := r.units[strings.ToLower(value)]
	if !ok {
		unit, ok = r.unitsByAbbr[strings.ToLower(value)]
	}

	if !ok {
		fail(template.ColUnit, domain.CodeUnknownUnit, "unit "+value+" does not exist")
		return ""
	}

	resolved := ""

	if unit.ID != part.BaseUnitID {
		for _, pack := range part.PackagingUnits {
			if pack.UnitID == unit.ID {
				resolved = unit.ID
			}
		}

		if resolved == "" {
			fail(template.ColUnit, domain.CodeUnitNotForPart, "unit "+value+" is not a unit of part "+part.SKU)
			return ""
		}
	}

	if scanned != resolved && scanned != "" {
		fail(template.ColUnit, domain.CodeUnitMismatch, "the barcode stands for a different unit than "+value)
		return ""
	}

	return resolved
}

// headerCheck makes the document-level columns of a receipt or count file
// agree: every non-empty value of a column must equal the first one.
type headerCheck struct {
	s     *sink
	first map[string]string
	at    map[string]int
}

func newHeaderCheck(s *sink) *headerCheck {
	return &headerCheck{s: s, first: map[string]string{}, at: map[string]int{}}
}

func (h *headerCheck) note(row int, column, value string) {
	if value == "" {
		return
	}

	first, seen := h.first[column]
	if !seen {
		h.first[column], h.at[column] = value, row

		return
	}

	if value != first {
		h.s.add(row, column, domain.CodeHeaderMismatch,
			column+" differs from row "+strconv.Itoa(h.at[column])+"; every row of the file must use the same value")
	}
}

// parseFactor normalizes a packaging factor with up to six decimals.
func parseFactor(s string) (string, bool) {
	if s == "" || !decimalPattern.MatchString(s) {
		return "", false
	}

	r, ok := new(big.Rat).SetString(s)
	if !ok || r.Sign() <= 0 {
		return "", false
	}

	scaled := new(big.Rat).Mul(r, big.NewRat(1_000_000, 1))
	if !scaled.IsInt() {
		return "", false
	}

	out := r.FloatString(6)
	out = strings.TrimRight(strings.TrimRight(out, "0"), ".")

	return out, true
}
