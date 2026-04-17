package repos

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var testDriverSeq uint64

type testSQLBehavior struct {
	mu sync.Mutex

	onQuery func(query string, args []driver.Value) (testQueryResult, error)
	onExec  func(query string, args []driver.Value) (driver.Result, error)

	beginErr    error
	commitErr   error
	rollbackErr error

	commitCount   int
	rollbackCount int
}

type testQueryResult struct {
	columns []string
	rows    [][]driver.Value
}

func newTestDB(t *testing.T, behavior *testSQLBehavior) *sql.DB {
	t.Helper()

	name := fmt.Sprintf("order_repos_test_driver_%d", atomic.AddUint64(&testDriverSeq, 1))
	sql.Register(name, &testDriver{behavior: behavior})

	db, err := sql.Open(name, "")
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

type testDriver struct {
	behavior *testSQLBehavior
}

func (d *testDriver) Open(_ string) (driver.Conn, error) {
	return &testConn{behavior: d.behavior}, nil
}

type testConn struct {
	behavior *testSQLBehavior
}

func (c *testConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, fmt.Errorf("prepare not supported")
}

func (c *testConn) Close() error {
	return nil
}

func (c *testConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *testConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	c.behavior.mu.Lock()
	defer c.behavior.mu.Unlock()

	if c.behavior.beginErr != nil {
		return nil, c.behavior.beginErr
	}

	return &testTx{behavior: c.behavior}, nil
}

func (c *testConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.behavior.onQuery == nil {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}

	result, err := c.behavior.onQuery(query, namedValues(args))
	if err != nil {
		return nil, err
	}

	return &testRows{
		columns: result.columns,
		rows:    result.rows,
	}, nil
}

func (c *testConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.behavior.onExec == nil {
		return nil, fmt.Errorf("unexpected exec: %s", query)
	}

	return c.behavior.onExec(query, namedValues(args))
}

type testTx struct {
	behavior *testSQLBehavior
}

func (tx *testTx) Commit() error {
	tx.behavior.mu.Lock()
	defer tx.behavior.mu.Unlock()

	tx.behavior.commitCount++

	return tx.behavior.commitErr
}

func (tx *testTx) Rollback() error {
	tx.behavior.mu.Lock()
	defer tx.behavior.mu.Unlock()

	tx.behavior.rollbackCount++

	return tx.behavior.rollbackErr
}

type testRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *testRows) Columns() []string {
	return r.columns
}

func (r *testRows) Close() error {
	return nil
}

func (r *testRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}

	for i := range r.rows[r.index] {
		dest[i] = normalizeRowValue(r.rows[r.index][i])
	}
	r.index++

	return nil
}

func normalizeRowValue(value driver.Value) driver.Value {
	if value == nil {
		return nil
	}

	if id, ok := value.(uuid.UUID); ok {
		return id.String()
	}

	return value
}

func namedValues(args []driver.NamedValue) []driver.Value {
	values := make([]driver.Value, len(args))
	for i := range args {
		values[i] = args[i].Value
	}

	return values
}

func queryHasName(query string, name string) bool {
	return strings.Contains(query, fmt.Sprintf("name: %s :", name))
}
