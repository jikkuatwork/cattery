package normalize

import "testing"

func TestNormalizeAcceptance(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "titles and currency",
			in:   "Dr. Smith paid $3.50",
			want: "Doctor Smith paid three dollars and fifty cents",
		},
		{
			name: "acronym",
			in:   "The IEEE standard",
			want: "The I E E E standard",
		},
		{
			name: "spoken as word",
			in:   "NASA launched",
			want: "NASA launched",
		},
		{
			name: "mixed case and possessive acronym",
			in:   "mRNA at MIT's lab",
			want: "M R N A at M I T's lab",
		},
		{
			name: "percent and cardinal",
			in:   "7.4% of 1,000",
			want: "seven point four percent of one thousand",
		},
		{
			name: "date",
			in:   "3/22/2026",
			want: "March twenty second, twenty twenty six",
		},
		{
			name: "time",
			in:   "3:30pm",
			want: "three thirty p m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Normalize(tt.in); got != tt.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
