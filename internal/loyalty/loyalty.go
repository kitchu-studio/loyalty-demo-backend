// Package loyalty holds the small pure rules of the points program, kept free of
// HTTP and database code so they are easy to test.
package loyalty

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
)

// Letters and digits that are hard to mix up when read aloud or typed from a
// receipt: no I, L, O, 0 or 1.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// PointsFor converts a bill into points: one point per bahtPerPoint baht, rounded down.
func PointsFor(amountBaht, bahtPerPoint int) int {
	if amountBaht <= 0 || bahtPerPoint <= 0 {
		return 0
	}
	return amountBaht / bahtPerPoint
}

// NewCode returns a random code of n characters from the readable alphabet.
func NewCode(n int) (string, error) {
	if n <= 0 {
		return "", errors.New("code length must be positive")
	}
	max := big.NewInt(int64(len(codeAlphabet)))
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(codeAlphabet[idx.Int64()])
	}
	return b.String(), nil
}

// NormalizeCode makes user input comparable: upper case, without spaces or dashes,
// so "k7p-2qm" and "K7P 2QM" both match "K7P2QM".
func NormalizeCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	return s
}
