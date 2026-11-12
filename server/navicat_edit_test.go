package server

import (
	"context"
	"database/sql"
	"gbaselite/executor"
	"gbaselite/storageengine"
	"io"
	"log"
	"net"
	"testing"
	"time"
)

type metadataScanBackend struct {
	storageengine.Engine
	scans int
}
type metadataScanTxn struct {
	storageengine.Txn
	backend *metadataScanBackend
}

func (b *metadataScanBackend) Begin(ctx context.Context) (storageengine.Txn, error) {
	tx, err := b.Engine.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &metadataScanTxn{Txn: tx, backend: b}, nil
}
func (t *metadataScanTxn) ScanRange(ctx context.Context, space string, r storageengine.KeyRange, visit func([]byte, []byte) error) error {
	t.backend.scans++
	return t.Txn.ScanRange(ctx, space, r, visit)
}

func TestNavicatTableEnumerationDoesNotScanRows(t *testing.T) {
	e, err := openTestEngine(t, t.TempDir(), "root", "secret")
	if err != nil {
		t.Fatal(err)
	}
	s := &executor.Session{}
	for _, q := range []string{"CREATE DATABASE grid", "CREATE TABLE grid.items(id INT PRIMARY KEY)", "INSERT INTO grid.items VALUES(1),(2)"} {
		if _, err := ExecuteCompatible(e, s, q); err != nil {
			t.Fatal(err)
		}
	}
	backend := &metadataScanBackend{Engine: e.Backend}
	e.Backend = backend
	for _, q := range []string{"SELECT TABLE_NAME,TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA='grid'", "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA='grid'"} {
		r, err := ExecuteCompatible(e, s, q)
		if err != nil || len(r.Rows) != 1 {
			t.Fatalf("%s: %v %v", q, r, err)
		}
	}
	if backend.scans != 0 {
		t.Fatalf("enumerating table names scanned rows %d times", backend.scans)
	}
	r, err := ExecuteCompatible(e, s, "SELECT TABLE_ROWS FROM information_schema.TABLES WHERE TABLE_SCHEMA='grid' AND TABLE_NAME='items'")
	if err != nil || len(r.Rows) != 1 || r.Rows[0][0] != int64(2) || backend.scans == 0 {
		t.Fatalf("live statistics: result=%+v scans=%d err=%v", r, backend.scans, err)
	}
}

func TestNavicatGridWritesAndCountsOverTCP(t *testing.T) {
	e, err := openTestEngine(t, t.TempDir(), "root", "secret")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &MySQLServer{Engine: e, Logger: log.New(io.Discard, "", 0)}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	db, err := sql.Open("mysql", "root:secret@tcp("+listener.Addr().String()+")/?timeout=3s&readTimeout=3s&writeTimeout=3s")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer func() {
		db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		<-done
	}()
	exec := func(q string, args ...any) sql.Result {
		t.Helper()
		r, err := db.Exec(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return r
	}
	exec("CREATE DATABASE grid")
	exec("USE grid")
	exec("CREATE TABLE items(id INT PRIMARY KEY AUTO_INCREMENT,label VARCHAR(40) NOT NULL,note TEXT)")
	count := func(want int) {
		t.Helper()
		for _, q := range []string{"SELECT COUNT(*) FROM items", "SELECT COUNT(1) AS num FROM `grid`.`items`", "SELECT COUNT(*) FROM (SELECT * FROM items) AS counted", "SELECT TABLE_ROWS FROM information_schema.TABLES WHERE TABLE_SCHEMA='grid' AND TABLE_NAME='items'"} {
			var got int
			if err := db.QueryRow(q).Scan(&got); err != nil || got != want {
				t.Fatalf("%s: count=%d want=%d err=%v", q, got, want, err)
			}
		}
	}
	count(0)
	inserted := exec("INSERT INTO items(label,note) VALUES(?,?)", "first", nil)
	id, err := inserted.LastInsertId()
	if err != nil || id != 1 {
		t.Fatalf("insert id=%d err=%v", id, err)
	}
	exec("INSERT INTO items(label) VALUES('second'),('third')")
	count(3)
	updated := exec("UPDATE `grid`.`items` SET `label`='edited' WHERE `id` <=> 1 AND `note` <=> NULL LIMIT 1")
	affected, err := updated.RowsAffected()
	if err != nil || affected != 1 {
		t.Fatalf("update affected=%d err=%v", affected, err)
	}
	var label string
	if err := db.QueryRow("SELECT label FROM items WHERE id=?", id).Scan(&label); err != nil || label != "edited" {
		t.Fatalf("label=%q err=%v", label, err)
	}
	count(3)
	exec("DELETE FROM items WHERE id=2 LIMIT 1")
	count(2)
	exec("SET autocommit=0")
	exec("INSERT INTO items(label) VALUES('pending')")
	var inTransaction int
	if err := db.QueryRow("SELECT COUNT(*) FROM items").Scan(&inTransaction); err != nil || inTransaction != 3 {
		t.Fatalf("transaction count=%d err=%v", inTransaction, err)
	}
	exec("ROLLBACK")
	exec("SET autocommit=1")
	count(2)
}
