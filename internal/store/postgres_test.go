package store

import "testing"

func TestBindPostgres(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"parameters", "SELECT * FROM users WHERE id=? AND email=?", "SELECT * FROM users WHERE id=$1 AND email=$2"},
		{"single quoted question mark", "SELECT '?' AS mark, id FROM users WHERE id=?", "SELECT '?' AS mark, id FROM users WHERE id=$1"},
		{"escaped quote", "SELECT 'it''s ?' AS value WHERE id=?", "SELECT 'it''s ?' AS value WHERE id=$1"},
		{"quoted identifier", "SELECT \"column?\" FROM sample WHERE id=?", "SELECT \"column?\" FROM sample WHERE id=$1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := bindPostgres(test.query); got != test.want {
				t.Fatalf("bindPostgres() = %q, want %q", got, test.want)
			}
		})
	}
}
