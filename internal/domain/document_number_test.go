package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/entity"
)

func TestDocumentsAreNumberedInTheSeriesOfTheirBusinessDate(t *testing.T) {
	doc := entity.NewDocument()
	doc.Date = time.Date(2025, 12, 30, 15, 0, 0, 0, time.UTC)
	require.Equal(t, doc.Date, numberPeriod(&doc), "a December document entered in January keeps its year")

	doc.Date = time.Time{}
	require.WithinDuration(t, time.Now(), numberPeriod(&doc), time.Minute, "a document without a date is numbered by today")
	require.WithinDuration(t, time.Now(), numberPeriod(struct{}{}), time.Minute)
}
