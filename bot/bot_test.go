package bot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-telegram/bot/models"

	tgmd "github.com/eekstunt/telegramify-markdown-go"
)

// TestToEntities checks the mapping from the markdown library's entities to
// Telegram Bot API entities, preserving offsets, lengths, and link/code fields.
func TestToEntities(t *testing.T) {
	in := []tgmd.Entity{
		{Type: tgmd.Bold, Offset: 0, Length: 5},
		{Type: tgmd.TextLink, Offset: 6, Length: 4, URL: "https://example.com"},
		{Type: tgmd.Pre, Offset: 11, Length: 8, Language: "go"},
	}

	out := toEntities(in)
	if len(out) != len(in) {
		t.Fatalf("got %d entities, want %d", len(out), len(in))
	}
	for i, e := range in {
		got := out[i]
		if got.Type != models.MessageEntityType(e.Type) {
			t.Errorf("entity %d type = %q, want %q", i, got.Type, e.Type)
		}
		if got.Offset != e.Offset || got.Length != e.Length {
			t.Errorf("entity %d offset/length = (%d,%d), want (%d,%d)",
				i, got.Offset, got.Length, e.Offset, e.Length)
		}
		if got.URL != e.URL {
			t.Errorf("entity %d URL = %q, want %q", i, got.URL, e.URL)
		}
		if got.Language != e.Language {
			t.Errorf("entity %d Language = %q, want %q", i, got.Language, e.Language)
		}
	}
}

// TestToEntitiesEmpty makes sure an empty entity slice maps cleanly.
func TestToEntitiesEmpty(t *testing.T) {
	if out := toEntities(nil); len(out) != 0 {
		t.Fatalf("got %d entities, want 0", len(out))
	}
}

func TestAllowedUsersRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allowed_users.txt")

	ids := map[int64]struct{}{42: {}, 7: {}, 1234567890: {}}
	if err := saveAllowedUsers(path, ids); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, fromFile, err := loadAllowedUsers(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !fromFile {
		t.Fatal("fromFile = false, want true")
	}
	if len(loaded) != len(ids) {
		t.Fatalf("loaded %d ids, want %d", len(loaded), len(ids))
	}
	for id := range ids {
		if _, ok := loaded[id]; !ok {
			t.Errorf("id %d missing after round trip", id)
		}
	}
}

func TestLoadAllowedUsersMissingFile(t *testing.T) {
	_, fromFile, err := loadAllowedUsers(filepath.Join(t.TempDir(), "nope.txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fromFile {
		t.Fatal("fromFile = true for a missing file, want false")
	}
}

func TestLoadAllowedUsersRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allowed_users.txt")
	for _, content := range []string{"abc", "1,-5", "1,0", "1,2x"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadAllowedUsers(path); err == nil {
			t.Errorf("content %q: expected an error, got nil", content)
		}
	}
}

func TestParseCommand(t *testing.T) {
	for _, tc := range []struct {
		text string
		cmd  string
		args int
	}{
		{"/add 123", "/add", 1},
		{"/model", "/model", 0},
		{"/start@SomeBot payload", "/start", 1},
		{"plain text", "", 0},
		{"  /remove   42  ", "/remove", 1},
	} {
		cmd, args := parseCommand(tc.text)
		if cmd != tc.cmd || len(args) != tc.args {
			t.Errorf("parseCommand(%q) = (%q, %d args), want (%q, %d args)",
				tc.text, cmd, len(args), tc.cmd, tc.args)
		}
	}
}
