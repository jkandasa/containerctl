package cmd

import "testing"

func TestDownSelectAll(t *testing.T) {
	// --all alone matches bare down: unfiltered, so host containers that
	// are no longer in the stack are still removed.
	if downSelectAll(true, nil, nil) {
		t.Fatal("--all alone must stay unfiltered")
	}
	if downSelectAll(false, nil, nil) {
		t.Fatal("bare down must stay unfiltered")
	}
	if !downSelectAll(true, []string{"web"}, nil) {
		t.Fatal("--all with names must keep the name filter")
	}
	if !downSelectAll(true, nil, []string{"tier=web"}) {
		t.Fatal("--all with labels must keep the label filter")
	}
	if downSelectAll(false, []string{"web"}, nil) {
		t.Fatal("names without --all must not set all")
	}
}
