package corpus

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ristir/urd/internal/config"
)

func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		h, err := os.UserHomeDir()
		if err != nil {
			h = os.Getenv("HOME")
		}
		return filepath.Join(h, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
	}
	return p
}

// matchSegments matches a pattern against a path segment by segment: "**" covers
// any number of segments, so the tail of a pattern may contain separators.
func matchSegments(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchSegments(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, err := filepath.Match(pat[0], name[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pat[1:], name[1:])
}

// ExpandGlob: filepath.Glob does not understand "**", so the recursive case is
// walked by hand segment by segment - a pattern may hold several "**".
func ExpandGlob(pattern string) []string {
	pattern = expandTilde(pattern)
	sep := string(os.PathSeparator)
	segments := strings.Split(pattern, sep)
	starAt := -1
	for i, seg := range segments {
		if seg == "**" {
			starAt = i
			break
		}
	}
	if starAt < 0 {
		matches, _ := filepath.Glob(pattern)
		return existingFiles(matches)
	}

	root := strings.Join(segments[:starAt], sep)
	if root == "" {
		if strings.HasPrefix(pattern, sep) {
			root = sep
		} else {
			// The pattern starts with "**" and no literal prefix: walk the process working
			// directory, not the root of the disk.
			root = "."
		}
	}
	tail := segments[starAt:]

	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		if matchSegments(tail, strings.Split(rel, sep)) {
			out = append(out, p)
		}
		return nil
	})
	return existingFiles(out)
}

func existingFiles(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() > 0 {
			out = append(out, p)
		}
	}
	return out
}

func Discover(s config.Sources) []string {
	var candidates []string
	if s.Auto {
		h, err := os.UserHomeDir()
		if err != nil {
			h = os.Getenv("HOME")
		}
		if hf := os.Getenv("HISTFILE"); hf != "" {
			candidates = append(candidates, hf)
		}
		candidates = append(candidates,
			filepath.Join(h, ".zsh_history"),
			filepath.Join(h, ".bash_history"),
		)
		candidates = append(candidates, ExpandGlob(filepath.Join(config.ImportedDir(), "*"))...)
	}
	for _, pattern := range s.Extra {
		candidates = append(candidates, ExpandGlob(pattern)...)
	}

	seen := make(map[string]bool, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, p := range existingFiles(candidates) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, abs)
	}
	return out
}
