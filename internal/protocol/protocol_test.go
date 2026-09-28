package protocol

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestNewlineDelimitedPackets(t *testing.T) {
	var stream bytes.Buffer
	if err := Write(&stream, Packet{Type: "login", Username: "Paulos"}); err != nil {
		t.Fatal(err)
	}
	if err := Write(&stream, Packet{Type: "chat", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	scanner := NewScanner(&stream)
	first, err := Read(scanner)
	if err != nil || first.Type != "login" || first.Username != "Paulos" {
		t.Fatalf("bad first packet: %#v, %v", first, err)
	}
	second, err := Read(scanner)
	if err != nil || second.Type != "chat" || second.Text != "hello" {
		t.Fatalf("bad second packet: %#v, %v", second, err)
	}
	if _, err := Read(scanner); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestUsernameAndOversizedFrame(t *testing.T) {
	for _, name := range []string{"Paulos", "miguel_03"} {
		if !ValidUsername(name) {
			t.Fatalf("valid username rejected: %q", name)
		}
	}
	for _, name := range []string{"", "has spaces", "åna", strings.Repeat("a", 25)} {
		if ValidUsername(name) {
			t.Fatalf("invalid username accepted: %q", name)
		}
	}
	scanner := NewScanner(strings.NewReader(strings.Repeat("x", MaxPacketBytes+100) + "\n"))
	if _, err := Read(scanner); err == nil {
		t.Fatal("oversized frame should be rejected")
	}
}
