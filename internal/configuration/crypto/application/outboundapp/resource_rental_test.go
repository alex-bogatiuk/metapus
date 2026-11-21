package outboundapp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddEnergySafetyMarginRoundsUp(t *testing.T) {
	assert.Equal(t, int64(16115), addEnergySafetyMargin(14650, 10))
	assert.Equal(t, int64(0), addEnergySafetyMargin(0, 10))
}
