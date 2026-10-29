package ports_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/HectorVeraO/initializer/tooling/generator/internal/ports"
)

func TestScanFindsTokensWithTheirBandOffsetAndLine(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []ports.Token
	}{
		{"empty", "", nil},
		{"six digits is not a token", "x 101320 y", nil},
		{"four digits is not a token", "x 1013 y", nil},
		{"after a dot", "host.10132", []ports.Token{{Start: 5, End: 10, Line: 1, Band: 101, Offset: 32}}},
		{"after a colon", "localhost:10120/", []ports.Token{{Start: 10, End: 15, Line: 1, Band: 101, Offset: 20}}},
		{"at buffer start", "10100 x", []ports.Token{{Start: 0, End: 5, Line: 1, Band: 101, Offset: 0}}},
		{"at buffer end", "x 10199", []ports.Token{{Start: 2, End: 7, Line: 1, Band: 101, Offset: 99}}},
		{"range of two tokens", "10100-10199", []ports.Token{
			{Start: 0, End: 5, Line: 1, Band: 101, Offset: 0},
			{Start: 6, End: 11, Line: 1, Band: 101, Offset: 99},
		}},
		{"line numbers", "a\n\nb 10133\n10134", []ports.Token{
			{Start: 5, End: 10, Line: 3, Band: 101, Offset: 33},
			{Start: 11, End: 16, Line: 4, Band: 101, Offset: 34},
		}},
		{"letters around digits", "v10132x", []ports.Token{{Start: 1, End: 6, Line: 1, Band: 101, Offset: 32}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ports.Scan([]byte(tc.src))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Scan(%q) = %+v, want %+v", tc.src, got, tc.want)
			}
		})
	}
}

func TestRewriteIdentityKeepsTheSourceBytes(t *testing.T) {
	src := []byte("port 10132 and 10120")
	got, err := ports.Rewrite(src, 101, 101, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("port 10132 and 10120")) {
		t.Fatalf("identity rewrite changed the bytes: %q", got)
	}
}

func TestRewriteMovesTheBandAndMapsOffsets(t *testing.T) {
	src := []byte("a 10100-10199 b http://localhost:10120/x \"10132\"")
	cases := []struct {
		name    string
		to      int
		offsets map[int]int
		want    string
	}{
		{"band only", 103, nil, "a 10300-10399 b http://localhost:10320/x \"10332\""},
		{"origin rule", 101, map[int]int{20: 0}, "a 10100-10199 b http://localhost:10100/x \"10132\""},
		{"band and origin rule", 654, map[int]int{20: 0}, "a 65400-65499 b http://localhost:65400/x \"65432\""},
		{"lowest band", 100, nil, "a 10000-10099 b http://localhost:10020/x \"10032\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ports.Rewrite(src, 101, tc.to, tc.offsets)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	if string(src) != "a 10100-10199 b http://localhost:10120/x \"10132\"" {
		t.Fatal("Rewrite changed its input")
	}
}

func TestRewriteRefusesWhatItCannotRewrite(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		from, to int
		offsets  map[int]int
		wantText string
	}{
		{"foreign band", "x 10332", 101, 102, nil, "line 1: token 10332 has band 103, not 101"},
		{"foreign band names its line", "ok 10100\n\nbad 10232", 101, 102, nil, "line 3"},
		{"target band too low", "10100", 101, 99, nil, "from 100 to 654"},
		{"target band too high", "10100", 101, 655, nil, "from 100 to 654"},
		{"identity with a band out of range", "10100", 50, 50, nil, "from 100 to 654"},
		{"source band too low", "10100", 99, 101, nil, "from 100 to 654"},
		{"offset mapped below zero", "10120", 101, 101, map[int]int{20: -1}, "outside 0-99"},
		{"offset mapped above 99", "10120", 101, 101, map[int]int{20: 100}, "outside 0-99"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ports.Rewrite([]byte(tc.src), tc.from, tc.to, tc.offsets)
			if err == nil {
				t.Fatalf("got %q, want an error", got)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("error %q lacks %q", err, tc.wantText)
			}
		})
	}
}

func TestRewriteReportsEveryProblemTogether(t *testing.T) {
	_, err := ports.Rewrite([]byte("10232\n10332"), 101, 102, nil)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"line 1", "line 2"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q lacks %q", err, want)
		}
	}
}

func FuzzScan(f *testing.F) {
	for _, seed := range []string{"", "10100", "10100-10199", "a1234567b", "x\n10132\n", "0000000000", "9999 99999 999999"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		previousEnd := 0
		for _, token := range ports.Scan(src) {
			if token.Start < previousEnd {
				t.Fatalf("token %+v overlaps or precedes the previous one", token)
			}
			previousEnd = token.End
			if token.End-token.Start != 5 {
				t.Fatalf("token %+v is not five bytes", token)
			}
			for _, c := range src[token.Start:token.End] {
				if c < '0' || c > '9' {
					t.Fatalf("token %+v holds a non-digit", token)
				}
			}
			if token.Start > 0 && src[token.Start-1] >= '0' && src[token.Start-1] <= '9' {
				t.Fatalf("token %+v has a digit before it", token)
			}
			if token.End < len(src) && src[token.End] >= '0' && src[token.End] <= '9' {
				t.Fatalf("token %+v has a digit after it", token)
			}
			wantLine := 1 + bytes.Count(src[:token.Start], []byte{'\n'})
			if token.Line != wantLine {
				t.Fatalf("token %+v on line %d, want %d", token, token.Line, wantLine)
			}
		}
	})
}

func FuzzRewrite(f *testing.F) {
	for _, seed := range []string{"", "10100", "10100-10199 10120", "x10132y\n10133", "123456 10120", "99999"} {
		f.Add([]byte(seed), uint16(103), uint8(20), uint8(0))
	}
	f.Fuzz(func(t *testing.T, src []byte, band uint16, from, to uint8) {
		target := ports.MinBand + int(band)%(ports.MaxBand-ports.MinBand+1)

		same, err := ports.Rewrite(src, ports.DefaultBand, ports.DefaultBand, nil)
		if err != nil {
			t.Fatalf("identity rewrite failed: %v", err)
		}
		if !bytes.Equal(same, src) {
			t.Fatal("identity rewrite changed the bytes")
		}

		offsets := map[int]int{int(from) % 100: int(to) % 100}
		out, err := ports.Rewrite(src, ports.DefaultBand, target, offsets)
		if err != nil {
			// Only a foreign band may fail: the mapped offset is always valid.
			for _, token := range ports.Scan(src) {
				if token.Band != ports.DefaultBand {
					return
				}
			}
			t.Fatalf("rewrite failed on a source of reference-band tokens: %v", err)
		}
		if len(out) != len(src) {
			t.Fatalf("length changed from %d to %d", len(src), len(out))
		}
		before, after := ports.Scan(src), ports.Scan(out)
		if len(before) != len(after) {
			t.Fatalf("token count changed from %d to %d", len(before), len(after))
		}
		inToken := make([]bool, len(src))
		for i, token := range after {
			if token.Band != target {
				t.Fatalf("output token %+v has band %d, want %d", token, token.Band, target)
			}
			wantOffset := before[i].Offset
			if mapped, found := offsets[wantOffset]; found {
				wantOffset = mapped
			}
			if token.Offset != wantOffset {
				t.Fatalf("output token %+v has offset %d, want %d", token, token.Offset, wantOffset)
			}
			for at := token.Start; at < token.End; at++ {
				inToken[at] = true
			}
		}
		for at := range src {
			if !inToken[at] && src[at] != out[at] {
				t.Fatalf("byte %d outside every token changed", at)
			}
		}
	})
}
