package bot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// utf16Units mirrors Telegram's length accounting for assertions.
func utf16Units(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func TestSplitMessageShortTextSingleChunk(t *testing.T) {
	chunks := splitMessage("hello world", 4096)
	if len(chunks) != 1 || chunks[0] != "hello world" {
		t.Fatalf("got %q, want single chunk %q", chunks, "hello world")
	}
}

func TestSplitMessageRespectsUTF16Limit(t *testing.T) {
	for name, text := range map[string]string{
		"ascii":   strings.Repeat("a", 10000),
		"emoji":   strings.Repeat("😀", 5000), // 2 UTF-16 units each
		"mixed":   strings.Repeat("a😀b ", 3000),
		"newline": strings.Repeat(strings.Repeat("x", 80)+"\n", 200),
	} {
		t.Run(name, func(t *testing.T) {
			chunks := splitMessage(text, 4096)
			var rebuilt strings.Builder
			for i, c := range chunks {
				if u := utf16Units(c); u > 4096 {
					t.Errorf("chunk %d is %d UTF-16 units, exceeds 4096", i, u)
				}
				if strings.TrimSpace(c) == "" {
					t.Errorf("chunk %d is whitespace-only", i)
				}
				rebuilt.WriteString(c)
			}
			if rebuilt.String() != text {
				t.Error("concatenated chunks do not reproduce the input")
			}
		})
	}
}

func TestSplitMessagePrefersNewlineBoundary(t *testing.T) {
	// One newline past the halfway point; the cut should land right after it.
	text := strings.Repeat("a", 3000) + "\n" + strings.Repeat("b", 3000)
	chunks := splitMessage(text, 4096)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if !strings.HasSuffix(chunks[0], "\n") {
		t.Error("first chunk does not end at the newline boundary")
	}
	if !strings.HasPrefix(chunks[1], "b") {
		t.Error("second chunk does not start after the newline")
	}
}

func TestSplitMessageDropsWhitespaceOnlyTail(t *testing.T) {
	text := strings.Repeat("a", 4096) + "\n \n"
	for i, c := range splitMessage(text, 4096) {
		if strings.TrimSpace(c) == "" {
			t.Errorf("chunk %d is whitespace-only", i)
		}
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
