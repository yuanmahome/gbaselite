package executor

import "testing"

func TestNavicatBaseTableColumnIdentity(t *testing.T) {
	e, s, run := rangeTestEngine(t)
	run("CREATE TABLE items(id INT PRIMARY KEY AUTO_INCREMENT, label VARCHAR(40) NOT NULL, note TEXT, UNIQUE KEY uq_label(label))")
	run("INSERT INTO items(label) VALUES('first'),('second')")
	for _, query := range []string{"SELECT * FROM items LIMIT 1000", "SELECT i.id AS item_id,i.label,i.note FROM items i ORDER BY i.id"} {
		r, err := e.Execute(s, query)
		if err != nil {
			t.Fatal(err)
		}
		for i, name := range []string{"id", "label", "note"} {
			c := r.Columns[i]
			if c.Schema != s.CurrentDatabase || c.Table != "items" || c.OriginalName != name {
				t.Fatalf("%s: column identity = %+v", query, c)
			}
		}
		if !r.Columns[0].PrimaryKey || !r.Columns[0].AutoIncrement || r.Columns[0].Nullable || !r.Columns[1].UniqueKey || r.Columns[1].Length != 40 || !r.Columns[2].Nullable {
			t.Fatalf("column flags = %+v", r.Columns)
		}
	}
	for _, query := range []string{"SELECT COUNT(*) FROM items", "SELECT id+1 FROM items", "SELECT DISTINCT id FROM items", "SELECT id FROM items GROUP BY id", "SELECT d.id FROM (SELECT id FROM items) d"} {
		r, err := e.Execute(s, query)
		if err != nil {
			t.Fatal(err)
		}
		if r.Columns[0].Table != "" || r.Columns[0].PrimaryKey {
			t.Fatalf("expression marked editable: %s: %+v", query, r.Columns)
		}
	}
}

// The reported Navicat grid uses a VARCHAR id without a declared primary key.
// It still needs an origin for every field so Navicat can match old values.
func TestNavicatGridWithoutPrimaryKey(t *testing.T) {
	e, s, run := rangeTestEngine(t)
	run("CREATE TABLE auth_app(id VARCHAR(64),app_name VARCHAR(64),app_desc TEXT,note TEXT)")
	run("INSERT INTO auth_app VALUES('app-1','first','description',NULL),('app-2','second','description',NULL)")
	result := run("SELECT * FROM auth_app LIMIT 0,1000")
	for i, name := range []string{"id", "app_name", "app_desc", "note"} {
		column := result.Columns[i]
		if column.Schema != s.CurrentDatabase || column.Table != "auth_app" || column.OriginalName != name || column.PrimaryKey {
			t.Fatalf("no-primary-key field identity: %+v", column)
		}
	}
	updated := run("UPDATE auth_app SET app_name='edited' WHERE id <=> 'app-1' AND app_name <=> 'first' AND app_desc <=> 'description' AND note <=> NULL LIMIT 1")
	if updated.AffectedRows != 1 {
		t.Fatalf("affected rows=%d", updated.AffectedRows)
	}
	result, err := e.Execute(s, "SELECT app_name FROM auth_app WHERE id='app-1'")
	if err != nil || len(result.Rows) != 1 || result.Rows[0][0] != "edited" {
		t.Fatalf("read back=%+v err=%v", result, err)
	}
}
