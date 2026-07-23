// mcprecall-go - Developed by acidvegas in Go (https://github.com/acidvegas)
// internal/format/format_test.go

package format

import "testing"

func TestBytes(t *testing.T) {
	cases := map[int]string{
		0:               "0B",
		512:             "512B",
		1023:            "1023B",
		1536:            "1.5KB",
		2 * 1024 * 1024: "2.0MB",
	}
	for in, want := range cases {
		if got := Bytes(in); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestRelativeTime(t *testing.T) {
	cases := map[int64]string{
		30 * 1000:               "just now",
		90 * 1000:               "1m ago",
		60 * 60 * 1000:          "1h ago",
		(90 * 60) * 1000:        "1h 30m ago",
		25 * 60 * 60 * 1000:     "1 day ago",
		3 * 24 * 60 * 60 * 1000: "3 days ago",
	}
	for in, want := range cases {
		if got := RelativeTime(in); got != want {
			t.Errorf("RelativeTime(%d) = %q, want %q", in, got, want)
		}
	}
}
