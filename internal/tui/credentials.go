package tui

import (
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// secretInput is never rendered or forwarded to a general-purpose text input.
// Bubble Tea owns the terminal in raw/no-echo mode. Paste events are discarded:
// a pasted newline must not submit credentials or become a later chat command.
type secretInput struct {
	values [3][]byte
	field  int
}

func (s *secretInput) clear() {
	for i := range s.values {
		clear(s.values[i])
		s.values[i] = nil
	}
	s.field = 0
}
func (s *secretInput) key(k tea.KeyPressMsg) {
	b := s.values[s.field]
	if b == nil {
		b = make([]byte, 0, 128)
	}
	if k.String() == "backspace" {
		if len(b) > 0 {
			_, n := utf8.DecodeLastRune(b)
			clear(b[len(b)-n:])
			b = b[:len(b)-n]
		}
	} else if k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) == 0 {
		for _, r := range k.Text {
			if !unicode.IsControl(r) && len(b)+utf8.RuneLen(r) <= 128 {
				b = utf8.AppendRune(b, r)
			}
		}
	}
	s.values[s.field] = b
}
