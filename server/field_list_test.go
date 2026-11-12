package server

import (
	"gbaselite/executor"
	"gbaselite/protocol"
	"net"
	"testing"
	"time"
)

func TestFieldListIncludesDefaults(t *testing.T) {
	e, err := openTestEngine(t, t.TempDir(), "root", "secret")
	if err != nil {
		t.Fatal(err)
	}
	session := &executor.Session{CurrentDatabase: "fields"}
	for _, q := range []string{"CREATE DATABASE fields", "CREATE TABLE items(id INT PRIMARY KEY,note VARCHAR(20) DEFAULT 'hello',empty_note VARCHAR(20) DEFAULT '',null_note TEXT)"} {
		if _, err := e.Execute(session, q); err != nil {
			t.Fatal(err)
		}
	}
	writer, reader := net.Pipe()
	defer writer.Close()
	defer reader.Close()
	writer.SetDeadline(time.Now().Add(3 * time.Second))
	reader.SetDeadline(time.Now().Add(3 * time.Second))
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv := &MySQLServer{Engine: e}
		srv.handleFieldList(&protocol.PacketConn{Conn: writer}, session, "items\x00")
	}()
	packets := &protocol.PacketConn{Conn: reader}
	for i, want := range []*string{nil, stringPtr("hello"), stringPtr(""), nil} {
		data, err := packets.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		pos := 0
		for j := 0; j < 6; j++ {
			n, err := protocol.ReadLenEncInteger(data, &pos)
			if err != nil {
				t.Fatal(err)
			}
			pos += int(n)
		}
		n, err := protocol.ReadLenEncInteger(data, &pos)
		if err != nil {
			t.Fatal(err)
		}
		pos += int(n)
		if pos >= len(data) {
			t.Fatalf("field %d missing COM_FIELD_LIST default", i)
		}
		if want == nil {
			if data[pos] != 0xfb || pos+1 != len(data) {
				t.Fatalf("field %d NULL default=%x", i, data[pos:])
			}
		} else {
			n, err := protocol.ReadLenEncInteger(data, &pos)
			if err != nil || pos+int(n) != len(data) || string(data[pos:]) != *want {
				t.Fatalf("field %d default=%x want=%q err=%v", i, data[pos:], *want, err)
			}
		}
	}
	eof, err := packets.ReadPacket()
	if err != nil || len(eof) != 5 || eof[0] != 0xfe {
		t.Fatalf("EOF=%x err=%v", eof, err)
	}
	<-done
	e.CloseSession(session)
}

func stringPtr(s string) *string { return &s }
