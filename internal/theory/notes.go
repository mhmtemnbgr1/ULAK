// Package theory nota adlarını (C4, F#5, Bb3 ...) frekanslara çevirir.
package theory

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// nota harfinin oktav içindeki yarım-ses konumu (C = 0)
var semitone = map[byte]int{
	'C': 0, 'D': 2, 'E': 4, 'F': 5, 'G': 7, 'A': 9, 'B': 11,
}

// MIDI nota numarasını frekansa çevirir. A4 (MIDI 69) = 440 Hz.
func MidiToFreq(midi int) float64 {
	return 440.0 * math.Pow(2, float64(midi-69)/12.0)
}

// NoteToMidi "C4", "F#5", "Bb3", "A4" gibi bir adı MIDI numarasına çevirir.
func NoteToMidi(name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("boş nota adı")
	}

	base, ok := semitone[name[0]&^0x20] // büyük harfe zorla
	if !ok {
		return 0, fmt.Errorf("geçersiz nota harfi: %q", name)
	}

	i := 1
	for i < len(name) {
		switch name[i] {
		case '#', 's', 'S':
			base++
			i++
		case 'b', 'B':
			base--
			i++
		default:
			goto octave
		}
	}
octave:
	if i >= len(name) {
		return 0, fmt.Errorf("oktav eksik: %q", name)
	}
	octave, err := strconv.Atoi(name[i:])
	if err != nil {
		return 0, fmt.Errorf("geçersiz oktav: %q", name)
	}

	// MIDI: C-1 = 0, dolayısıyla midi = (octave+1)*12 + base
	return (octave+1)*12 + base, nil
}

// NoteToFreq nota adını doğrudan frekansa çevirir.
func NoteToFreq(name string) (float64, error) {
	midi, err := NoteToMidi(name)
	if err != nil {
		return 0, err
	}
	return MidiToFreq(midi), nil
}
