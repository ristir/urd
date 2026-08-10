package histfile

import "testing"

func TestFlattenJoinsContinuations(t *testing.T) {
	in := "ansible-playbook rate-limit.yml \\\n    -l cache-02.zone01.us.lab \\\n    -e APP_VERSION=7eb83dc3"
	want := "ansible-playbook rate-limit.yml -l cache-02.zone01.us.lab -e APP_VERSION=7eb83dc3"
	if got := Flatten(in); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFlattenSingleLineUnchanged(t *testing.T) {
	in := "kubectl get pods -A"
	if got := Flatten(in); got != in {
		t.Fatalf("got %q, want %q", got, in)
	}
}

func TestFlattenKeepsInnerSpacesAndQuotes(t *testing.T) {
	in := "grep -R 'два  слова' notes \\\n  -l"
	want := "grep -R 'два  слова' notes -l"
	if got := Flatten(in); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFlattenDropsTrailingBackslashWithoutNewline(t *testing.T) {
	in := `echo test \`
	if got := Flatten(in); got != "echo test" {
		t.Fatalf("got %q, want %q", got, "echo test")
	}
}

// Measured on the real corpus: 166 of the 191 commands with repeated spaces look like this.
func TestSqueezeSpacesCollapsesTyposOutsideQuotes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"cat  /tmp/urd", "cat /tmp/urd"},
		{"kubectl  get-contexts", "kubectl get-contexts"},
		{"ks3 get pod -n  stars-00000-integrations", "ks3 get pod -n stars-00000-integrations"},
	}
	for _, c := range cases {
		if got := SqueezeSpaces(c.in); got != c.want {
			t.Fatalf("SqueezeSpaces(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The other 25 from the same count: repeated spaces inside quotes are the command, byte-for-byte.
func TestSqueezeSpacesLeavesQuotedRunsByteForByte(t *testing.T) {
	cases := []string{
		`psql --dbname "$CONN" -c " WITH cur AS (SELECT unnest(ARRAY[1,2])::int AS id) SELECT  id FROM cur"`,
		`ssh lb-01.zone01.us.lab 'curl -s http://127.0.0.1:8500/v1/catalog/service/x  | jq .'`,
		`sed 's/a  b/x/'`,
		`grep 'два  слова' notes`,
	}
	for _, in := range cases {
		if got := SqueezeSpaces(in); got != in {
			t.Fatalf("SqueezeSpaces(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestSqueezeSpacesRefusesUnbalancedQuotes(t *testing.T) {
	in := `echo "unterminated  quote`
	if got := SqueezeSpaces(in); got != in {
		t.Fatalf("SqueezeSpaces(%q) = %q, want unchanged (unbalanced quotes)", in, got)
	}
}
