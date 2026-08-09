package main

import "testing"

func TestWantsSelfCheck(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{}, false},
		{[]string{"--help"}, false},
		{[]string{"--self-check"}, true},
		{[]string{"-self-check"}, true},
		{[]string{"/self-check"}, true},
		{[]string{"--foo", "--self-check"}, true},
	}
	for _, tc := range cases {
		if got := wantsSelfCheck(tc.args); got != tc.want {
			t.Fatalf("wantsSelfCheck(%v)=%v, want %v", tc.args, got, tc.want)
		}
	}
}
