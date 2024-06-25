package main

import (
	"testing"
)

func Test_clearMentions(t *testing.T) {
	const text = `Hello [Thomas](twist-mention://123), how are you?`
	const want = "Hello Thomas, how are you?"
	got := clearMentions(text)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
