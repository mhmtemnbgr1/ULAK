// Package song basit metin tabanlı şarkı dosyalarını okur ve çalar.
//
// Dosya formatı (örnek):
//
//	# title: Twinkle Twinkle Little Star
//	# tempo: 120
//	C4 C4 G4 G4 A4 A4 G4:2
//	F4 F4 E4 E4 D4 D4 C4:2
//
// - '#' ile başlayan satırlar üstbilgi (title, tempo) veya yorumdur.
// - Her nota "AD:SÜRE" biçimindedir; :SÜRE isteğe bağlıdır (varsayılan 1 vuruş).
// - '-' bir es (sessizlik) demektir, ör. "-:2".
package song

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"piano/internal/theory"
)

// Note çalınacak tek bir olay: frekans (0 ise es) ve saniye cinsinden süre.
type Note struct {
	Name    string  // gösterim için orijinal ad ("C4", "-")
	Midi    int     // MIDI nota numarası (es ise -1)
	Freq    float64 // 0 => es
	Seconds float64
}

// Song bir şarkının başlığı ve nota dizisidir.
type Song struct {
	Title string
	Tempo int // BPM
	Notes []Note
}

// Load bir şarkı dosyasını ayrıştırır.
func Load(path string) (*Song, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := &Song{Title: "İsimsiz", Tempo: 120}
	var tokens []string

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			parseHeader(line, s)
			continue
		}
		tokens = append(tokens, strings.Fields(line)...)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	beat := 60.0 / float64(s.Tempo) // bir vuruşun saniye karşılığı
	for _, tok := range tokens {
		name, beats := splitToken(tok)
		n := Note{Name: name, Midi: -1, Seconds: beats * beat}
		if name != "-" {
			if midi, err := theory.NoteToMidi(name); err == nil {
				n.Midi = midi
				n.Freq = theory.MidiToFreq(midi)
			}
		}
		s.Notes = append(s.Notes, n)
	}
	return s, nil
}

func parseHeader(line string, s *Song) {
	body := strings.TrimSpace(strings.TrimPrefix(line, "#"))
	key, val, ok := strings.Cut(body, ":")
	if !ok {
		return
	}
	key = strings.ToLower(strings.TrimSpace(key))
	val = strings.TrimSpace(val)
	switch key {
	case "title", "başlık", "baslik":
		s.Title = val
	case "tempo", "bpm":
		if t, err := strconv.Atoi(val); err == nil && t > 0 {
			s.Tempo = t
		}
	}
}

// splitToken "C4:2" -> ("C4", 2). Süre yoksa 1 vuruş.
func splitToken(tok string) (name string, beats float64) {
	name, dur, ok := strings.Cut(tok, ":")
	if !ok {
		return tok, 1
	}
	if b, err := strconv.ParseFloat(dur, 64); err == nil && b > 0 {
		return name, b
	}
	return name, 1
}
