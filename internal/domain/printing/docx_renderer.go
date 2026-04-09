package printing

import (
	"fmt"
	"io"

	"github.com/gomutex/godocx"
	"github.com/gomutex/godocx/wml/stypes"
)

// RenderDOCX writes the PrintTable data as a Word .docx file to w.
func RenderDOCX(w io.Writer, data *PrintData) error {
	t := data.Table
	if t == nil {
		return fmt.Errorf("PrintData.Table is nil — DOCX export requires pre-formatted table data")
	}

	doc, err := godocx.NewDocument()
	if err != nil {
		return fmt.Errorf("create docx document: %w", err)
	}
	defer func() { _ = doc.Close() }()

	// ── Title ─────────────────────────────────────────────────────────────
	titlePara := doc.AddParagraph(t.Title)
	titlePara.Justification(stypes.JustificationCenter)

	// ── Subtitle ──────────────────────────────────────────────────────────
	subPara := doc.AddParagraph(t.Subtitle)
	subPara.Justification(stypes.JustificationCenter)

	doc.AddEmptyParagraph()

	// ── Header fields (2-column table matching HTML layout) ──────────────
	if len(t.HeaderRows) > 0 {
		headerTbl := doc.AddTable()
		for _, headerRow := range t.HeaderRows {
			r := headerTbl.AddRow()
			for i, hf := range headerRow {
				if i > 0 {
					// gap column between left and right pair
					r.AddCell()
				}
				labelCell := r.AddCell()
				labelCell.AddParagraph(hf.Label)
				valCell := r.AddCell()
				valCell.AddParagraph(hf.Value)
			}
		}
	}

	doc.AddEmptyParagraph()

	// ── Line items table ──────────────────────────────────────────────────
	tbl := doc.AddTable()

	// Header row
	hRow := tbl.AddRow()
	for _, col := range t.Columns {
		cell := hRow.AddCell()
		cell.AddParagraph(col)
	}

	// Data rows
	for _, row := range t.Rows {
		r := tbl.AddRow()
		for _, val := range row.Values {
			cell := r.AddCell()
			cell.AddParagraph(val)
		}
	}

	doc.AddEmptyParagraph()

	// ── Totals ────────────────────────────────────────────────────────────
	for _, total := range t.Totals {
		p := doc.AddEmptyParagraph()
		p.Justification(stypes.JustificationRight)
		p.AddText(total.Label + " ").Color("444444")
		run := p.AddText(total.Value)
		run.Bold(true)
		if total.Grand {
			run.Size(14)
		}
	}

	doc.AddEmptyParagraph()
	doc.AddEmptyParagraph()

	// ── Signatures (layout-driven from PrintSignatureBlock) ──────────────
	if sb := t.SignatureBlock; sb != nil && len(sb.Entries) > 0 {
		switch sb.Layout {
		case SignatureLayoutHorizontal:
			sigTbl := doc.AddTable()

			// Row 1: role labels
			labelRow := sigTbl.AddRow()
			for _, entry := range sb.Entries {
				cell := labelRow.AddCell()
				cell.AddParagraph(entry.Role)
			}

			// Row 2: signature lines
			lineRow := sigTbl.AddRow()
			for range sb.Entries {
				cell := lineRow.AddCell()
				cell.AddParagraph("______________________________")
			}

			// Row 3: hint text (from entry, not hardcoded)
			hintRow := sigTbl.AddRow()
			for _, entry := range sb.Entries {
				cell := hintRow.AddCell()
				p := cell.AddParagraph(entry.Hint)
				p.Justification(stypes.JustificationCenter)
			}

		default: // vertical
			for _, entry := range sb.Entries {
				p := doc.AddEmptyParagraph()
				p.AddText(entry.Role).Color("444444").Size(9)
				doc.AddParagraph("______________________________")
				if entry.Hint != "" {
					hp := doc.AddEmptyParagraph()
					hp.AddText(entry.Hint).Color("888888").Size(8)
					hp.Justification(stypes.JustificationCenter)
				}
				doc.AddEmptyParagraph()
			}
		}
	}

	// ── Footer ────────────────────────────────────────────────────────────
	footerPara := doc.AddParagraph("Сформировано автоматически системой Metapus")
	footerPara.Justification(stypes.JustificationCenter)

	return doc.Write(w)
}
