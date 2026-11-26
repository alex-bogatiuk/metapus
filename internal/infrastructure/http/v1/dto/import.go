package dto

// ImportPreviewRequest is a file to load into a catalog: its name (the
// extension picks the format, .xlsx or .csv) and its content in base64.
type ImportPreviewRequest struct {
	FileName string `json:"fileName" binding:"required,max=255"`
	Content  string `json:"content" binding:"required"`
}

// ImportField is a catalog field a column may be loaded into.
type ImportField struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Required  bool   `json:"required"`
	Reference bool   `json:"reference"`
}

// ImportColumn is a column of the file and the field it is suggested for
// ("" when none fits).
type ImportColumn struct {
	Header string `json:"header"`
	Field  string `json:"field"`
}

// ImportPreviewResponse shows the file's columns with suggested fields, the
// catalog's fields, the first rows and the number of rows.
type ImportPreviewResponse struct {
	Columns []ImportColumn `json:"columns"`
	Fields  []ImportField  `json:"fields"`
	Rows    [][]string     `json:"rows"`
	Total   int            `json:"total"`
}

// ImportCatalogRequest loads a file: mapping takes a column header to the
// field it fills (columns left out are not loaded). With matchByCode a row
// whose code an item has updates that item. dryRun checks every row and
// stores nothing.
type ImportCatalogRequest struct {
	FileName    string            `json:"fileName" binding:"required,max=255"`
	Content     string            `json:"content" binding:"required"`
	Mapping     map[string]string `json:"mapping" binding:"required,min=1"`
	MatchByCode bool              `json:"matchByCode"`
	DryRun      bool              `json:"dryRun"`
}

// ImportRowError is a row that cannot be loaded: its line in the file, the
// column at fault ("" for the row) and why.
type ImportRowError struct {
	Line    int    `json:"line"`
	Column  string `json:"column"`
	Message string `json:"message"`
}

// ImportCatalogResponse is the outcome: how many items the file creates and
// updates, the rows at fault, and whether it was stored (only a run without
// dryRun and without errors is).
type ImportCatalogResponse struct {
	Created int              `json:"created"`
	Updated int              `json:"updated"`
	Errors  []ImportRowError `json:"errors"`
	DryRun  bool             `json:"dryRun"`
	Applied bool             `json:"applied"`
}
