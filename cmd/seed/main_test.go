package main

import (
	"os"
	"testing"
)

// SEC-22: the password comes from SEED_PASSWORD or stdin, never from argv.
func TestReadPassword(t *testing.T) {
	pipe := func(s string) *os.File {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		w.WriteString(s)
		w.Close()
		t.Cleanup(func() { r.Close() })
		return r
	}

	if got, err := readPassword("iz-okruzenja-123", pipe(""), os.Stderr); err != nil || got != "iz-okruzenja-123" {
		t.Errorf("env: %q, %v", got, err)
	}
	if got, err := readPassword("", pipe("sa-stdin-12345\n"), os.Stderr); err != nil || got != "sa-stdin-12345" {
		t.Errorf("stdin: %q, %v", got, err)
	}
	if got, err := readPassword("", pipe("bez-novog-reda1"), os.Stderr); err != nil || got != "bez-novog-reda1" {
		t.Errorf("stdin without newline: %q, %v", got, err)
	}
	if _, err := readPassword("", pipe("kratka\n"), os.Stderr); err == nil {
		t.Error("short password accepted")
	}
	if _, err := readPassword("", pipe(""), os.Stderr); err == nil {
		t.Error("empty stdin accepted")
	}
}
