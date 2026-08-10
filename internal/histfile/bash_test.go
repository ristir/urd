package histfile

import "testing"

func TestParseBashWithTimestamps(t *testing.T) {
	data := []byte("#1700000000\nll\n#1700000060\npwd\n")
	got := ParseBash(data, "b")
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Cmd != "ll" || got[0].TS != 1700000000 {
		t.Fatalf("entry 0 = %+v", got[0])
	}
	if got[1].TS != 1700000060 {
		t.Fatalf("entry 1 = %+v", got[1])
	}
}

func TestParseBashWithoutTimestamps(t *testing.T) {
	data := []byte("ll\npwd\nls -al\n")
	got := ParseBash(data, "b")
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}
	for i, e := range got {
		if e.TS != 0 {
			t.Fatalf("entry %d has TS %d, want 0", i, e.TS)
		}
		if e.Ord != i {
			t.Fatalf("entry %d has Ord %d, want %d", i, e.Ord, i)
		}
	}
}

func TestParseBashIgnoresRealComments(t *testing.T) {
	data := []byte("#ansible-playbook site.yml\nll\n")
	got := ParseBash(data, "b")
	if len(got) != 2 || got[0].Cmd != "#ansible-playbook site.yml" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseBashTimestampClassification(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want []struct {
			cmd string
			ts  int64
		}
	}{
		{
			name: "space before number is a command",
			data: []byte("# 8080\n"),
			want: []struct {
				cmd string
				ts  int64
			}{
				{cmd: "# 8080", ts: 0},
			},
		},
		{
			name: "zero is a command not a timestamp",
			data: []byte("#0\n"),
			want: []struct {
				cmd string
				ts  int64
			}{
				{cmd: "#0", ts: 0},
			},
		},
		{
			name: "negative number is a command not a timestamp",
			data: []byte("#-5\n"),
			want: []struct {
				cmd string
				ts  int64
			}{
				{cmd: "#-5", ts: 0},
			},
		},
		{
			name: "number with trailing text is a command",
			data: []byte("#1700000000 extra\n"),
			want: []struct {
				cmd string
				ts  int64
			}{
				{cmd: "#1700000000 extra", ts: 0},
			},
		},
		{
			name: "valid timestamp",
			data: []byte("#1700000000\necho test\n"),
			want: []struct {
				cmd string
				ts  int64
			}{
				{cmd: "echo test", ts: 1700000000},
			},
		},
		{
			name: "ansible comment is a command",
			data: []byte("#ansible-playbook site.yml\n"),
			want: []struct {
				cmd string
				ts  int64
			}{
				{cmd: "#ansible-playbook site.yml", ts: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseBash(tt.data, "test")
			if len(got) != len(tt.want) {
				t.Fatalf("got %d entries, want %d", len(got), len(tt.want))
			}
			for i, e := range got {
				if e.Cmd != tt.want[i].cmd {
					t.Errorf("entry %d: cmd = %q, want %q", i, e.Cmd, tt.want[i].cmd)
				}
				if e.TS != tt.want[i].ts {
					t.Errorf("entry %d: TS = %d, want %d", i, e.TS, tt.want[i].ts)
				}
			}
		})
	}
}
