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
