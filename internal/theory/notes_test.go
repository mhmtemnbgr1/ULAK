package theory

import (
	"math"
	"testing"
)

func TestNoteToFreq(t *testing.T) {
	cases := map[string]float64{
		"A4":  440.0,
		"C4":  261.63,
		"C#4": 277.18,
		"Db4": 277.18,
		"A5":  880.0,
		"C0":  16.35,
	}
	for name, want := range cases {
		got, err := NoteToFreq(name)
		if err != nil {
			t.Fatalf("%s: beklenmeyen hata: %v", name, err)
		}
		if math.Abs(got-want) > 0.5 {
			t.Errorf("%s: got %.2f, want %.2f", name, got, want)
		}
	}
}

func TestNoteToMidi(t *testing.T) {
	if m, _ := NoteToMidi("C4"); m != 60 {
		t.Errorf("C4 midi = %d, want 60", m)
	}
	if m, _ := NoteToMidi("A4"); m != 69 {
		t.Errorf("A4 midi = %d, want 69", m)
	}
}
