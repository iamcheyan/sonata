package main

import (
	"reflect"
	"testing"
)

func TestReorderFlagsKeepsPositionalsAndFlagValues(t *testing.T) {
	got := reorderFlags([]string{"/music/record", "--music", "/library", "--data-dir=/state", "-v"})
	want := []string{"--music", "/library", "--data-dir=/state", "-v", "/music/record"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reorderFlags = %#v, want %#v", got, want)
	}
}

func TestParseDuration(t *testing.T) {
	cases := map[string]int64{
		"never": 0,
		"30s":   30,
		"1m":    60,
		"3m":    180,
		"5m":    300,
	}
	for in, seconds := range cases {
		if got := int64(parseDuration(in).Seconds()); got != seconds {
			t.Errorf("parseDuration(%q) = %d seconds, want %d", in, got, seconds)
		}
	}
}
