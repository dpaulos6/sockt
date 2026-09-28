package client_test

import (
	"io"
	"log"
	"net"
	"path/filepath"
	"testing"
	"time"

	"sockt/internal/client"
	"sockt/internal/protocol"
	"sockt/internal/server"
)

func waitConnected(t *testing.T, c *client.Client) {
	t.Helper()
	timeout := time.NewTimer(4 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case e, ok := <-c.Incoming():
			if !ok {
				t.Fatal("connection closed")
			}
			if e.Online {
				return
			}
		case <-timeout.C:
			t.Fatal("timed out waiting for login")
		}
	}
}
func waitMessage(t *testing.T, c *client.Client) protocol.Message {
	t.Helper()
	timeout := time.NewTimer(4 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case e, ok := <-c.Incoming():
			if !ok {
				t.Fatal("events closed")
			}
			if e.Packet.Type == "message" && e.Packet.Message != nil {
				return *e.Packet.Message
			}
		case <-timeout.C:
			t.Fatal("timed out waiting for server message")
		}
	}
}
func TestClientsShareBroadcastWithRealCredentials(t *testing.T) {
	tmp := t.TempDir()
	users := filepath.Join(tmp, "users.json")
	pa, _ := server.Invite(users, "Paulos")
	mi, _ := server.Invite(users, "Miguel")
	srv, err := server.New(filepath.Join(tmp, "messages.json"), users, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()
	go srv.Serve(lis)
	a := client.New(lis.Addr().String(), "Paulos", pa, 0, nil)
	defer a.Close()
	b := client.New(lis.Addr().String(), "Miguel", mi, 0, nil)
	defer b.Close()
	waitConnected(t, a)
	waitConnected(t, b)
	if err := a.SendMessage("hey Miguel"); err != nil {
		t.Fatal(err)
	}
	x, y := waitMessage(t, a), waitMessage(t, b)
	if x.ID == 0 || x.ID != y.ID || x.Username != "Paulos" || x.Text != y.Text {
		t.Fatalf("authoritative messages differ %+v %+v", x, y)
	}
}
