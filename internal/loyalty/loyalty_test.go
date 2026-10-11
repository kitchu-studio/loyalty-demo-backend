package loyalty

import (
	"strings"
	"testing"
)

func TestPointsFor(t *testing.T) {
	cases := []struct{ amount, per, want int }{
		{250, 10, 25},
		{99, 10, 9},
		{9, 10, 0},
		{0, 10, 0},
		{-50, 10, 0},
		{100, 0, 0},
	}
	for _, c := range cases {
		if got := PointsFor(c.amount, c.per); got != c.want {
			t.Errorf("PointsFor(%d,%d) = %d, want %d", c.amount, c.per, got, c.want)
		}
	}
}

func TestNewCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		c, err := NewCode(6)
		if err != nil {
			t.Fatal(err)
		}
		if len(c) != 6 {
			t.Fatalf("len = %d", len(c))
		}
		for _, r := range c {
			if !strings.ContainsRune(codeAlphabet, r) {
				t.Fatalf("unexpected character %q in %q", r, c)
			}
		}
		seen[c] = true
	}
	if len(seen) < 495 {
		t.Errorf("too many repeated codes: %d unique of 500", len(seen))
	}
	if _, err := NewCode(0); err == nil {
		t.Error("expected error for length 0")
	}
}

func TestNormalizeCode(t *testing.T) {
	for _, in := range []string{"k7p2qm", " K7P-2QM ", "k7p 2qm"} {
		if got := NormalizeCode(in); got != "K7P2QM" {
			t.Errorf("NormalizeCode(%q) = %q", in, got)
		}
	}
}
