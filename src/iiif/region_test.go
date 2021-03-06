package iiif

import (
	"testing"

	"github.com/uoregon-libraries/gopkg/assert"
)

func TestRegionTypePercent(t *testing.T) {
	r := StringToRegion("pct:41.6,7.5,40,70")
	assert.True(r.Type == RTPercent, "r.Type == RTPercent", t)
	assert.Equal(41.6, r.X, "r.X", t)
	assert.Equal(7.5, r.Y, "r.Y", t)
	assert.Equal(40.0, r.W, "r.W", t)
	assert.Equal(70.0, r.H, "r.H", t)
}

func TestRegionTypePixels(t *testing.T) {
	r := StringToRegion("10,10,40,70")
	assert.True(r.Valid(), "r.Valid()", t)
	assert.True(r.Type == RTPixel, "r.Type == RTPixel", t)
	assert.Equal(10.0, r.X, "r.X", t)
	assert.Equal(10.0, r.Y, "r.Y", t)
	assert.Equal(40.0, r.W, "r.W", t)
	assert.Equal(70.0, r.H, "r.H", t)
}

func TestInvalidRegion(t *testing.T) {
	r := StringToRegion("10,10,0,70")
	assert.True(!r.Valid(), "!r.Valid()", t)
	r = StringToRegion("10,10,40,0")
	assert.True(!r.Valid(), "!r.Valid()", t)
	r = Region{}
	assert.True(!r.Valid(), "!r.Valid()", t)
}

func TestRegionTypeFull(t *testing.T) {
	r := StringToRegion("full")
	assert.True(r.Type == RTFull, "r.Type == RTFull", t)
}

func TestRegionTypeSquare(t *testing.T) {
	r := StringToRegion("square")
	assert.True(r.Type == RTSquare, "r.Type == RTSquare", t)
}
