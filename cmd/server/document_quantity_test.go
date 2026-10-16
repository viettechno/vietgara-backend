package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDocumentQuantity(t *testing.T) {
	t.Parallel()

	entered := "10"

	assert.Equal(t, "240 (10 case)", documentQuantity("240", &entered, "case"))
	assert.Equal(t, "240", documentQuantity("240", nil, ""), "the base unit prints as is")
	assert.Equal(t, "240", documentQuantity("240", &entered, ""), "without a unit name there is nothing to add")
}
