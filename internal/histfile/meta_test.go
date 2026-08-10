package histfile

import "testing"

func TestUnmetafyCyrillic(t *testing.T) {
	in := []byte{0xD0, 0x83, 0xBD, 0xD0, 0xB0, 0xD0, 0xB2, 0xD1, 0x83, 0xAB, 0xD0, 0xBA, 0xD0, 0xB8}
	got := string(Unmetafy(in))
	if got != "Навыки" {
		t.Fatalf("got %q, want %q", got, "Навыки")
	}
}

func TestUnmetafyPlainASCIIUnchanged(t *testing.T) {
	in := []byte("ansible-playbook site.yml")
	if got := string(Unmetafy(in)); got != string(in) {
		t.Fatalf("got %q, want %q", got, in)
	}
}

func TestUnmetafyTrailingMetaByteKept(t *testing.T) {
	in := []byte{0x61, 0x83}
	if got := Unmetafy(in); len(got) != 2 || got[1] != 0x83 {
		t.Fatalf("got % x, want 61 83", got)
	}
}
