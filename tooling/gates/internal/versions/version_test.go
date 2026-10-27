package versions_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/gates/internal/versions"
)

func triple(r *rand.Rand) ([3]int, string) {
	var parts [3]int
	for i := range parts {
		parts[i] = r.Intn(30)
	}
	return parts, fmt.Sprintf("%d.%d.%d", parts[0], parts[1], parts[2])
}

func tupleOrder(a, b [3]int) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func TestCompareVersionsIsAntisymmetricAndNumeric(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 2000; i++ {
		left, leftText := triple(r)
		right, rightText := triple(r)
		forward, err := versions.CompareVersions(leftText, rightText)
		if err != nil {
			t.Fatal(err)
		}
		backward, err := versions.CompareVersions(rightText, leftText)
		if err != nil {
			t.Fatal(err)
		}
		if forward != -backward {
			t.Fatalf("%s vs %s: %d and %d", leftText, rightText, forward, backward)
		}
		if forward != tupleOrder(left, right) {
			t.Fatalf("%s vs %s: got %d, tuple order %d", leftText, rightText, forward, tupleOrder(left, right))
		}
	}
}

func TestCompareVersionsIsNotStringOrder(t *testing.T) {
	order, err := versions.CompareVersions("0.10.0", "0.9.0")
	if err != nil || order != 1 {
		t.Fatalf("got %d, %v", order, err)
	}
}

func TestCompareVersionsRejectsInvalid(t *testing.T) {
	for _, bad := range []string{"", "1", "1.0", "01.0.0", "1.0.0.0", "1.0.0-rc", " 1.0.0", "1.0.0\n", "-1.0.0", "a.b.c"} {
		_, err := versions.CompareVersions(bad, "1.0.0")
		if err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func FuzzCompareVersions(f *testing.F) {
	f.Add("1.2.3", "1.2.4")
	f.Add("01.0.0", "0.0.0")
	f.Add("99999999999999999999.0.0", "0.0.0")
	f.Fuzz(func(t *testing.T, a, b string) {
		forward, errA := versions.CompareVersions(a, b)
		backward, errB := versions.CompareVersions(b, a)
		if (errA == nil) != (errB == nil) {
			t.Fatalf("asymmetric validity for %q, %q", a, b)
		}
		if errA == nil && forward != -backward {
			t.Fatalf("%q vs %q: %d and %d", a, b, forward, backward)
		}
	})
}
