package aliased

import (
	"crypto/rand"
	_ "embed"
	mrand "math/rand"
)

// Fill shows two packages that both declare rand, kept apart by an alias.
func Fill(b []byte) int {
	_, _ = rand.Read(b)

	return mrand.Int()
}
