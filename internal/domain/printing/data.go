package printing

// PrintData is the context passed to every print template.
type PrintData struct {
	// FormLabel is the form title, e.g. "Инвойс". Defaults to the form label.
	FormLabel string
	// Doc is an optional template-specific view model for custom templates.
	Doc any
	// Table is the format-agnostic, pre-formatted form content. Built-in HTML
	// templates and the DOCX renderer both render it, so formats stay identical.
	Table *PrintTable
}
