// Package importing reads the tables people load into catalogs: an XLSX
// workbook's first sheet, or a CSV file as Excel saves it (UTF-8 with or
// without BOM, or Windows-1251; separated by semicolons, commas or tabs).
package importing

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
)

// Limits of one load: larger files are split by the user.
const (
	MaxFileBytes     = 10 << 20
	MaxUnzipBytes    = 64 << 20
	MaxUnzipXMLBytes = 32 << 20
	MaxRows          = 10000
)

// Table is a file's header and data rows, cells trimmed; empty rows are
// left out, and short rows are padded to the header's width.
type Table struct {
	Header []string
	Rows   []Row
}

// Row is a data row and its line in the file (from 1, the header's line
// before it), for the user to find it.
type Row struct {
	Line  int
	Cells []string
}

// Read reads the table of the file named name: .xlsx, or .csv and .txt.
func Read(name string, data []byte) (*Table, error) {
	if len(data) == 0 {
		return nil, apperror.NewValidation("Файл пуст")
	}
	if len(data) > MaxFileBytes {
		return nil, apperror.NewValidation(fmt.Sprintf("Файл больше %d МБ: разделите его на части", MaxFileBytes>>20))
	}
	var lines []line
	var err error
	switch strings.ToLower(filepath.Ext(name)) {
	case ".xlsx":
		lines, err = readXLSX(data)
	case ".csv", ".txt":
		lines, err = readCSV(data)
	default:
		return nil, apperror.NewValidation("Загрузить можно файл XLSX или CSV")
	}
	if err != nil {
		return nil, err
	}
	return tableOf(lines)
}

// line is a file's line with its number.
type line struct {
	number int
	cells  []string
}

func tableOf(lines []line) (*Table, error) {
	table := &Table{}
	for _, l := range lines {
		cells := l.cells
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}
		if isEmpty(cells) {
			continue
		}
		if table.Header == nil {
			table.Header = cells
			continue
		}
		if len(table.Rows) == MaxRows {
			return nil, apperror.NewValidation(fmt.Sprintf("В файле больше %d строк: разделите его на части", MaxRows))
		}
		row := make([]string, len(table.Header))
		copy(row, cells)
		table.Rows = append(table.Rows, Row{Line: l.number, Cells: row})
	}
	if table.Header == nil {
		return nil, apperror.NewValidation("В файле нет строки заголовков")
	}
	seen := map[string]bool{}
	for i, h := range table.Header {
		if h == "" {
			return nil, apperror.NewValidation(fmt.Sprintf("У колонки %d нет заголовка", i+1))
		}
		key := strings.ToLower(h)
		if seen[key] {
			return nil, apperror.NewValidation(fmt.Sprintf("Заголовок «%s» повторяется", h))
		}
		seen[key] = true
	}
	return table, nil
}

func isEmpty(cells []string) bool {
	for _, c := range cells {
		if c != "" {
			return false
		}
	}
	return true
}

func readXLSX(data []byte) ([]line, error) {
	book, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{
		UnzipSizeLimit:    MaxUnzipBytes,
		UnzipXMLSizeLimit: MaxUnzipXMLBytes,
	})
	if err != nil {
		return nil, apperror.NewValidation("Файл не читается как XLSX").WithCause(err)
	}
	defer func() { _ = book.Close() }()
	sheets := book.GetSheetList()
	if len(sheets) == 0 {
		return nil, apperror.NewValidation("В книге нет листов")
	}
	rows, err := book.Rows(sheets[0])
	if err != nil {
		return nil, apperror.NewValidation("Лист не читается").WithCause(err)
	}
	defer func() { _ = rows.Close() }()

	lines := make([]line, 0, 256)
	lineNo := 0
	nonEmpty := 0
	for rows.Next() {
		lineNo++
		cells, colErr := rows.Columns()
		if colErr != nil {
			return nil, apperror.NewValidation("Лист не читается").WithCause(colErr)
		}
		trimmed := make([]string, len(cells))
		for j := range cells {
			trimmed[j] = strings.TrimSpace(cells[j])
		}
		if !isEmpty(trimmed) {
			nonEmpty++
			// 1 header row + MaxRows data rows; stop streaming before exhausting memory.
			if nonEmpty > MaxRows+1 {
				return nil, apperror.NewValidation(fmt.Sprintf("В файле больше %d строк: разделите его на части", MaxRows))
			}
		}
		lines = append(lines, line{number: lineNo, cells: trimmed})
	}
	if err := rows.Error(); err != nil {
		return nil, apperror.NewValidation("Лист не читается").WithCause(err)
	}
	return lines, nil
}

func readCSV(data []byte) ([]line, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		decoded, err := charmap.Windows1251.NewDecoder().Bytes(data)
		if err != nil {
			return nil, apperror.NewValidation("Кодировка файла не распознана: сохраните его в UTF-8").WithCause(err)
		}
		data = decoded
	}
	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = separator(data)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	var lines []line
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, apperror.NewValidation("Файл не читается как CSV").WithCause(err)
		}
		number, _ := reader.FieldPos(0)
		lines = append(lines, line{number: number, cells: record})
	}
	return lines, nil
}

// separator is the one of ; , and tab the first line has most of.
func separator(data []byte) rune {
	first, _, _ := bytes.Cut(data, []byte("\n"))
	best, count := ';', -1
	for _, c := range []rune{';', ',', '\t'} {
		if n := bytes.Count(first, []byte(string(c))); n > count {
			best, count = c, n
		}
	}
	return best
}
