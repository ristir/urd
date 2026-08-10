package histfile

// meta is zsh's service byte (Meta): the byte after it is stored as XOR 0x20.
const meta = 0x83

// Unmetafy undoes zsh's metafication. A histfile cannot be read as plain text:
// bytes 0x80-0x9F are stored as a pair (0x83, b^0x20), and Cyrillic breaks without this.
func Unmetafy(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == meta && i+1 < len(b) {
			i++
			out = append(out, b[i]^0x20)
			continue
		}
		out = append(out, b[i])
	}
	return out
}
