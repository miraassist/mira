package main

import (
	"reflect"
	"testing"

	"github.com/miraassist/mira/internal/config"
	"github.com/miraassist/mira/internal/ha/structure"
)

// HP-05 There is no own structure block
func TestHP05_ThereIsNoOwnStructureBlock(t *testing.T) {
	// Given the house structure is built from the registries
	// Then it has no text for the prompt
	if _, ok := reflect.TypeOf(structure.Snapshot{}).FieldByName("Block"); ok {
		t.Error("the house structure has a block of its own")
	}
	// And the config has no assist_prompt_server
	if _, err := config.Parse([]byte("ha:\n  assist_prompt_server: ha\n"), func(string) (string, bool) { return "", false }); err == nil {
		t.Error("assist_prompt_server is still a setting")
	}
}
