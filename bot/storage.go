package bot

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// allowedUsersFile persists the whitelist across restarts as a single line
// of comma-separated Telegram user IDs.
const allowedUsersFile = "allowed_users.txt"

// loadAllowedUsers reads the comma-separated whitelist file. The boolean is
// false when the file does not exist yet.
func loadAllowedUsers(path string) (map[int64]struct{}, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}

	ids := make(map[int64]struct{})
	for field := range strings.SplitSeq(string(data), ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		id, err := strconv.ParseInt(field, 10, 64)
		if err != nil || id <= 0 {
			return nil, false, fmt.Errorf("invalid user ID %q in %s", field, path)
		}
		ids[id] = struct{}{}
	}
	return ids, true, nil
}

// saveAllowedUsers writes the whitelist as sorted comma-separated IDs,
// replacing the file atomically so a crash cannot leave it truncated.
func saveAllowedUsers(path string, ids map[int64]struct{}) error {
	sorted := make([]int64, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	slices.Sort(sorted)

	parts := make([]string, len(sorted))
	for i, id := range sorted {
		parts[i] = strconv.FormatInt(id, 10)
	}
	line := strings.Join(parts, ",") + "\n"

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(line), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmp, path, err)
	}
	return nil
}
