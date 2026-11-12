package executor

import (
	"context"
	"gbaselite/storageengine"
	"path/filepath"
	"strings"
	"testing"
)

type statisticsCountingBackend struct {
	storageengine.Engine
	scans int
}
type statisticsCountingTxn struct {
	storageengine.Txn
	backend *statisticsCountingBackend
}

func (b *statisticsCountingBackend) Begin(ctx context.Context) (storageengine.Txn, error) {
	tx, err := b.Engine.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &statisticsCountingTxn{Txn: tx, backend: b}, nil
}
func (t *statisticsCountingTxn) ScanRange(ctx context.Context, space string, r storageengine.KeyRange, visit func([]byte, []byte) error) error {
	t.backend.scans++
	return t.Txn.ScanRange(ctx, space, r, visit)
}

func TestTableStatisticsCacheTracksCommittedSnapshot(t *testing.T) {
	e, _, run := rangeTestEngine(t)
	run("CREATE TABLE items(id INT PRIMARY KEY, label VARCHAR(40))")
	run("INSERT INTO items VALUES(1,'one'),(2,'two')")
	backend := &statisticsCountingBackend{Engine: e.Backend}
	e.Backend = backend
	check := func(want int64) {
		t.Helper()
		stats, err := e.TableStatistics(context.Background(), "test", "items")
		if err != nil || stats.Rows != want {
			t.Fatalf("stats=%+v err=%v want rows=%d", stats, err, want)
		}
	}
	check(2)
	scans := backend.scans
	check(2)
	if backend.scans != scans {
		t.Fatal("unchanged snapshot rescanned table")
	}
	run("INSERT INTO items VALUES(3,'three')")
	check(3)
	if backend.scans == scans {
		t.Fatal("write did not invalidate statistics")
	}
	run("BEGIN")
	run("DELETE FROM items WHERE id=1")
	check(3)
	run("ROLLBACK")
	check(3)
	run("DELETE FROM items WHERE id=1")
	check(2)
	run("DROP TABLE items")
	run("CREATE TABLE items(id INT PRIMARY KEY)")
	check(0)
}

func TestTableStatisticsCacheAfterRestore(t *testing.T) {
	e, _, run := rangeTestEngine(t)
	run("CREATE TABLE items(id INT PRIMARY KEY)")
	run("INSERT INTO items VALUES(1)")
	backup := strings.ReplaceAll(filepath.Join(t.TempDir(), "backup"), "\\", "/")
	run("BACKUP MVCC TO '" + backup + "'")
	run("INSERT INTO items VALUES(2)")
	before, err := e.TableStatistics(context.Background(), "test", "items")
	if err != nil || before.Rows != 2 {
		t.Fatalf("before restore: %+v %v", before, err)
	}
	run("RESTORE MVCC FROM '" + backup + "'")
	after, err := e.TableStatistics(context.Background(), "test", "items")
	if err != nil || after.Rows != 1 {
		t.Fatalf("after restore: %+v %v", after, err)
	}
}
