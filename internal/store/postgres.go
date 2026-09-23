package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// PostgreSQL uses $1, $2, ... parameters. Keeping the domain queries written
// with neutral ? markers makes them readable while this small adapter performs
// the PostgreSQL binding at the database boundary.
func bindPostgres(query string) string {
	var out strings.Builder
	out.Grow(len(query) + 16)
	parameter := 1
	inSingleQuote := false
	inDoubleQuote := false

	for i := 0; i < len(query); i++ {
		char := query[i]
		switch char {
		case '\'':
			if !inDoubleQuote {
				if inSingleQuote && i+1 < len(query) && query[i+1] == '\'' {
					out.WriteByte(char)
					out.WriteByte(query[i+1])
					i++
					continue
				}
				inSingleQuote = !inSingleQuote
			}
			out.WriteByte(char)
		case '"':
			if !inSingleQuote {
				if inDoubleQuote && i+1 < len(query) && query[i+1] == '"' {
					out.WriteByte(char)
					out.WriteByte(query[i+1])
					i++
					continue
				}
				inDoubleQuote = !inDoubleQuote
			}
			out.WriteByte(char)
		case '?':
			if inSingleQuote || inDoubleQuote {
				out.WriteByte(char)
				continue
			}
			out.WriteByte('$')
			out.WriteString(strconv.Itoa(parameter))
			parameter++
		default:
			out.WriteByte(char)
		}
	}
	return out.String()
}

func (s *Store) execContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, bindPostgres(query), args...)
}

func (s *Store) queryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, bindPostgres(query), args...)
}

func (s *Store) queryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, bindPostgres(query), args...)
}

type postgresTx struct{ *sql.Tx }

func (s *Store) beginTx(ctx context.Context, options *sql.TxOptions) (*postgresTx, error) {
	tx, err := s.db.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &postgresTx{Tx: tx}, nil
}

func (tx *postgresTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.Tx.ExecContext(ctx, bindPostgres(query), args...)
}

func (tx *postgresTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return tx.Tx.QueryContext(ctx, bindPostgres(query), args...)
}

func (tx *postgresTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return tx.Tx.QueryRowContext(ctx, bindPostgres(query), args...)
}
