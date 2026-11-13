package types

// PaletteColor names one colour of the shared interface palette. The value is a
// key, not a colour code: the frontend draws each key with shades tuned for the
// light and the dark theme. The empty value means "not chosen"; the interface
// then picks a stable colour itself.
type PaletteColor string

const (
	PaletteNone   PaletteColor = ""
	PaletteRed    PaletteColor = "red"
	PaletteOrange PaletteColor = "orange"
	PaletteAmber  PaletteColor = "amber"
	PaletteYellow PaletteColor = "yellow"
	PaletteLime   PaletteColor = "lime"
	PaletteGreen  PaletteColor = "green"
	PaletteTeal   PaletteColor = "teal"
	PaletteSky    PaletteColor = "sky"
	PaletteBlue   PaletteColor = "blue"
	PaletteIndigo PaletteColor = "indigo"
	PaletteViolet PaletteColor = "violet"
	PalettePink   PaletteColor = "pink"
	PaletteGray   PaletteColor = "gray"
)

// PaletteChoice is a palette colour with its name for people.
type PaletteChoice struct {
	Color PaletteColor
	Label string
}

// PaletteChoices lists the palette in the order a picker shows it.
var PaletteChoices = []PaletteChoice{
	{PaletteRed, "Красный"},
	{PaletteOrange, "Оранжевый"},
	{PaletteAmber, "Янтарный"},
	{PaletteYellow, "Жёлтый"},
	{PaletteLime, "Лаймовый"},
	{PaletteGreen, "Зелёный"},
	{PaletteTeal, "Бирюзовый"},
	{PaletteSky, "Голубой"},
	{PaletteBlue, "Синий"},
	{PaletteIndigo, "Индиго"},
	{PaletteViolet, "Фиолетовый"},
	{PalettePink, "Розовый"},
	{PaletteGray, "Серый"},
}

// Valid reports whether c is a palette colour or empty.
func (c PaletteColor) Valid() bool {
	if c == PaletteNone {
		return true
	}
	for _, choice := range PaletteChoices {
		if choice.Color == c {
			return true
		}
	}
	return false
}
