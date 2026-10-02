package song

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"piano/internal/theory"
)

// midiNote ayrıştırma sırasında tick cinsinden bir nota olayıdır.
type midiNote struct {
	on, off int
	key     int
}

type tempoChange struct {
	tick int
	usPQ int // çeyrek nota başına mikrosaniye
}

// LoadMIDI standart MIDI dosyasından (format 0/1) çalınabilir bir melodi
// çıkarır. Süreler tempo haritasından hesaplandığı için ritim dosyadakiyle
// birebir aynıdır. Birden çok kanal varsa melodi olma ihtimali en yüksek olan
// seçilir ve akorlarda yalnız en tiz nota tutulur.
func LoadMIDI(data []byte, fallbackTitle string) (*Song, error) {
	r := &midiReader{b: data}
	if string(r.take(4)) != "MThd" {
		return nil, errors.New("MIDI başlığı bulunamadı")
	}
	hdr := r.take(int(r.u32()))
	if r.err != nil || len(hdr) < 6 {
		return nil, errors.New("MIDI başlığı bozuk")
	}
	ntracks := int(binary.BigEndian.Uint16(hdr[2:4]))
	div := int(binary.BigEndian.Uint16(hdr[4:6]))
	if div&0x8000 != 0 || div == 0 {
		return nil, errors.New("SMPTE zamanlı MIDI desteklenmiyor")
	}

	var tempos []tempoChange
	addTempo := func(c tempoChange) { tempos = append(tempos, c) }
	groups := map[int][]midiNote{} // iz*16+kanal -> notalar
	title := ""

	for t := 0; t < ntracks; t++ {
		if string(r.take(4)) != "MTrk" {
			break
		}
		body := r.take(int(r.u32()))
		if r.err != nil {
			break
		}
		if name := parseTrack(body, t, addTempo, groups); title == "" {
			title = name
		}
	}
	if len(groups) == 0 {
		return nil, errors.New("MIDI içinde çalınabilir nota yok")
	}
	sort.SliceStable(tempos, func(i, j int) bool { return tempos[i].tick < tempos[j].tick })
	if len(tempos) == 0 || tempos[0].tick != 0 {
		tempos = append([]tempoChange{{0, 500000}}, tempos...)
	}

	// tick -> saniye (tempo haritası boyunca)
	secs := func(tick int) float64 {
		var s float64
		last, us := 0, tempos[0].usPQ
		for _, tc := range tempos[1:] {
			if tc.tick >= tick {
				break
			}
			s += float64(tc.tick-last) * float64(us) / float64(div) / 1e6
			last, us = tc.tick, tc.usPQ
		}
		return s + float64(tick-last)*float64(us)/float64(div)/1e6
	}

	notes := groups[pickMelody(groups)]
	sort.SliceStable(notes, func(i, j int) bool {
		if notes[i].on != notes[j].on {
			return notes[i].on < notes[j].on
		}
		return notes[i].key > notes[j].key // aynı anda en tiz nota önde
	})

	s := &Song{Title: title, Tempo: 60000000 / tempos[0].usPQ}
	if s.Title == "" {
		s.Title = fallbackTitle
	}
	const minDur = 0.08
	prevOn := -1
	for _, n := range notes {
		if n.on == prevOn { // akor: yalnız en tiz nota
			continue
		}
		prevOn = n.on
		start := secs(n.on)
		dur := secs(n.off) - start
		if dur < minDur {
			dur = minDur
		}
		s.Notes = append(s.Notes, Note{
			Name:    fmt.Sprintf("%d", n.key),
			Midi:    n.key,
			Freq:    theory.MidiToFreq(n.key),
			Start:   start,
			Seconds: dur,
		})
	}
	if len(s.Notes) == 0 {
		return nil, errors.New("MIDI içinde çalınabilir nota yok")
	}
	return s, nil
}

// pickMelody notaların en çok olduğu gruplar arasından ortalama perdesi en
// yüksek olanı seçer; akompanyman genelde daha pestir.
func pickMelody(groups map[int][]midiNote) int {
	most := 0
	for _, g := range groups {
		if len(g) > most {
			most = len(g)
		}
	}
	best, bestAvg := -1, -1.0
	for k, g := range groups {
		if len(g)*2 < most {
			continue
		}
		var sum float64
		for _, n := range g {
			sum += float64(n.key)
		}
		avg := sum / float64(len(g))
		if avg > bestAvg || (avg == bestAvg && k < best) {
			best, bestAvg = k, avg
		}
	}
	return best
}

// parseTrack tek bir izi çözer; izin adını döndürür.
func parseTrack(b []byte, track int, addTempo func(tempoChange), groups map[int][]midiNote) string {
	r := &midiReader{b: b}
	var (
		tick    int
		status  byte
		name    string
		pending = map[[2]int]int{} // (kanal,nota) -> açılış tick'i
	)
	add := func(ch, key, on, off int) {
		if ch == 9 { // 10. kanal davuldur
			return
		}
		gk := track*16 + ch
		groups[gk] = append(groups[gk], midiNote{on: on, off: off, key: key})
	}

	for r.err == nil && r.pos < len(b) {
		tick += r.varint()
		if r.peek()&0x80 != 0 {
			status = r.byte()
		} // yoksa çalışan durum (running status)

		switch {
		case status == 0xFF:
			typ := r.byte()
			data := r.take(r.varint())
			switch typ {
			case 0x03:
				if name == "" {
					name = string(data)
				}
			case 0x51:
				if len(data) == 3 {
					addTempo(tempoChange{tick, int(data[0])<<16 | int(data[1])<<8 | int(data[2])})
				}
			case 0x2F:
				return name
			}
		case status == 0xF0 || status == 0xF7:
			r.take(r.varint())
		default:
			ch := int(status & 0x0F)
			switch status & 0xF0 {
			case 0x90, 0x80:
				key, vel := int(r.byte()), int(r.byte())
				k := [2]int{ch, key}
				if status&0xF0 == 0x90 && vel > 0 {
					if _, open := pending[k]; !open {
						pending[k] = tick
					}
				} else if on, ok := pending[k]; ok {
					delete(pending, k)
					add(ch, key, on, tick)
				}
			case 0xC0, 0xD0:
				r.byte()
			default: // 0xA0, 0xB0, 0xE0: iki veri baytı
				r.byte()
				r.byte()
			}
		}
	}
	// Kapanmamış notaları izin sonunda bitir.
	for k, on := range pending {
		add(k[0], k[1], on, tick)
	}
	return name
}

// midiReader taşma korumalı basit bir bayt okuyucudur.
type midiReader struct {
	b   []byte
	pos int
	err error
}

func (r *midiReader) take(n int) []byte {
	if n < 0 || r.pos+n > len(r.b) {
		r.err = errors.New("MIDI dosyası beklenenden kısa")
		r.pos = len(r.b)
		return nil
	}
	out := r.b[r.pos : r.pos+n]
	r.pos += n
	return out
}

func (r *midiReader) byte() byte {
	if b := r.take(1); b != nil {
		return b[0]
	}
	return 0
}

func (r *midiReader) peek() byte {
	if r.pos < len(r.b) {
		return r.b[r.pos]
	}
	return 0
}

func (r *midiReader) u32() uint32 {
	if b := r.take(4); b != nil {
		return binary.BigEndian.Uint32(b)
	}
	return 0
}

func (r *midiReader) varint() int {
	v := 0
	for i := 0; i < 4; i++ {
		b := r.byte()
		v = v<<7 | int(b&0x7F)
		if b&0x80 == 0 {
			break
		}
	}
	return v
}
