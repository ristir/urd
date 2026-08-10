package histfile

// A histfile does not declare its format, so the first recognised record decides.
func Parse(data []byte, source string) []Entry {
	for _, line := range splitLines(string(Unmetafy(data))) {
		if line == "" {
			continue
		}
		if _, ok := jsonlHeader(line); ok {
			// If the first line fooled the detector, fall back rather than lose the source.
			if got := ParseJSONL(data, source); len(got) > 0 {
				return got
			}
			return ParseZsh(data, source)
		}
		if _, _, _, ok := zshHeader(line); ok {
			return ParseZsh(data, source)
		}
		if _, ok := bashTimestamp(line); ok {
			return ParseBash(data, source)
		}
		break
	}
	return ParseZsh(data, source)
}
