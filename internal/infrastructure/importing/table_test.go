package importing_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"

	"github.com/alex-bogatiuk/metapus/internal/infrastructure/importing"
)

func TestReadCSVAsExcelSavesIt(t *testing.T) {
	t.Parallel()
	text := "Наименование;Артикул;Ед.\r\nМасло 5W-30;152648;шт.\r\n\r\nКолодки;0 986;компл.\r\n"
	cp1251, err := charmap.Windows1251.NewEncoder().String(text)
	require.NoError(t, err)
	for name, data := range map[string][]byte{
		"utf8":     []byte(text),
		"utf8 bom": append([]byte("\xef\xbb\xbf"), text...),
		"cp1251":   []byte(cp1251),
	} {
		table, err := importing.Read("items.csv", data)
		require.NoError(t, err, name)
		require.Equal(t, []string{"Наименование", "Артикул", "Ед."}, table.Header, name)
		require.Len(t, table.Rows, 2, name)
		require.Equal(t, importing.Row{Line: 4, Cells: []string{"Колодки", "0 986", "компл."}}, table.Rows[1], name)
	}
	table, err := importing.Read("items.csv", []byte("code,name\nA,\"Масло, 1 л\"\nB"))
	require.NoError(t, err)
	require.Equal(t, []string{"A", "Масло, 1 л"}, table.Rows[0].Cells)
	require.Equal(t, []string{"B", ""}, table.Rows[1].Cells, "a short row is padded")
}

func TestReadXLSX(t *testing.T) {
	t.Parallel()
	book := excelize.NewFile()
	require.NoError(t, book.SetSheetRow("Sheet1", "A1", &[]any{"Код", "Наименование"}))
	require.NoError(t, book.SetSheetRow("Sheet1", "A2", &[]any{"IT-1", "Масло"}))
	var buf bytes.Buffer
	require.NoError(t, book.Write(&buf))
	table, err := importing.Read("Items.XLSX", buf.Bytes())
	require.NoError(t, err)
	require.Equal(t, []string{"Код", "Наименование"}, table.Header)
	require.Equal(t, []string{"IT-1", "Масло"}, table.Rows[0].Cells)
}

func TestReadRefusesWhatItCannotLoad(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		file string
		data string
		want string
	}{
		"format":    {"items.pdf", "x", "XLSX или CSV"},
		"empty":     {"items.csv", "", "Файл пуст"},
		"no header": {"items.csv", ";;\n", "нет строки заголовков"},
		"blank":     {"items.csv", "a;;c\n1;2;3", "нет заголовка"},
		"twice":     {"items.csv", "Код;код\n1;2", "повторяется"},
	} {
		_, err := importing.Read(c.file, []byte(c.data))
		require.ErrorContains(t, err, c.want, name)
	}
}
