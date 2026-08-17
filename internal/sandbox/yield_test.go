package sandbox

import "testing"

func TestExecYieldTimeMS(t *testing.T) {
	tests := []struct{ requested, want int }{
		{0, 10_000}, {1, 250}, {250, 250}, {15_000, 15_000}, {60_000, 30_000},
	}
	for _, test := range tests {
		if got := ExecYieldTimeMS(test.requested); got != test.want {
			t.Fatalf("ExecYieldTimeMS(%d) = %d, want %d", test.requested, got, test.want)
		}
	}
}

func TestStdinYieldTimeMS(t *testing.T) {
	if got := StdinYieldTimeMS("input", 0, 300_000); got != 250 {
		t.Fatalf("non-empty default = %d, want 250", got)
	}
	if got := StdinYieldTimeMS("input", 60_000, 300_000); got != 30_000 {
		t.Fatalf("non-empty maximum = %d, want 30000", got)
	}
	if got := StdinYieldTimeMS("", 0, 300_000); got != 5_000 {
		t.Fatalf("empty default = %d, want 5000", got)
	}
	if got := StdinYieldTimeMS("", 1_000, 300_000); got != 5_000 {
		t.Fatalf("empty minimum = %d, want 5000", got)
	}
	if got := StdinYieldTimeMS("", 600_000, 300_000); got != 300_000 {
		t.Fatalf("empty configured maximum = %d, want 300000", got)
	}
}
