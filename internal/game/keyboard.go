package game

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	text "github.com/hajimehoshi/ebiten/v2/text/v2"

	"piano/internal/song"
)

// Klavyenin görünen genişliği iki kademelidir: bir oktav (8 beyaz tuş, geniş
// şeritler) ya da iki oktav (15 beyaz tuş). Şarkı moduna girerken parçanın
// aralığına göre seçilir; serbest çalmada her zaman geniş olanı kullanılır.
const (
	spanOctave = 12 // 8 beyaz tuş
	spanFull   = 24 // 15 beyaz tuş
)

// binding bir bilgisayar tuşunu, temel oktava göre yarım ses uzaklığına bağlar.
type binding struct {
	offset int
	key    ebiten.Key
	label  string
}

// Standart "sanal piyano" düzeni: alt sıra pes oktav, üst sıra tiz oktav.
// Siyah tuşlar, gerçek piyanodaki gibi bir üst sıraya denk gelir.
var bindings = []binding{
	// pes oktav
	{0, ebiten.KeyZ, "Z"}, {1, ebiten.KeyS, "S"},
	{2, ebiten.KeyX, "X"}, {3, ebiten.KeyD, "D"},
	{4, ebiten.KeyC, "C"},
	{5, ebiten.KeyV, "V"}, {6, ebiten.KeyG, "G"},
	{7, ebiten.KeyB, "B"}, {8, ebiten.KeyH, "H"},
	{9, ebiten.KeyN, "N"}, {10, ebiten.KeyJ, "J"},
	{11, ebiten.KeyM, "M"},
	// tiz oktav
	{12, ebiten.KeyQ, "Q"}, {13, ebiten.KeyDigit2, "2"},
	{14, ebiten.KeyW, "W"}, {15, ebiten.KeyDigit3, "3"},
	{16, ebiten.KeyE, "E"},
	{17, ebiten.KeyR, "R"}, {18, ebiten.KeyDigit5, "5"},
	{19, ebiten.KeyT, "T"}, {20, ebiten.KeyDigit6, "6"},
	{21, ebiten.KeyY, "Y"}, {22, ebiten.KeyDigit7, "7"},
	{23, ebiten.KeyU, "U"},
	{24, ebiten.KeyI, "I"},
}

// isBlack bir yarım sesin siyah tuşa denk gelip gelmediğini söyler.
func isBlack(semitone int) bool {
	switch ((semitone % 12) + 12) % 12 {
	case 1, 3, 6, 8, 10:
		return true
	}
	return false
}

var noteLetters = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// noteName MIDI numarasını "C4", "F#5" gibi bir ada çevirir.
func noteName(midi int) string {
	return fmt.Sprintf("%s%d", noteLetters[((midi%12)+12)%12], midi/12-1)
}

// pianoKey ekrandaki tek bir tuştur.
type pianoKey struct {
	offset int  // temel oktava göre yarım ses
	black  bool
	label  string     // üzerindeki bilgisayar tuşu
	key    ebiten.Key // bağlı bilgisayar tuşu

	x, w float64 // ekran konumu (layout hesaplar)
	glow float64 // 0..1, basılınca 1 olur ve sönümlenir
	held bool
}

// keyboard ekranın altındaki piyanodur.
type keyboard struct {
	base   int // en soldaki tuşun MIDI numarası (her zaman bir do)
	span   int // en pes ile en tiz tuş arasındaki yarım ses farkı
	keys   []pianoKey
	whites int // görünen beyaz tuş sayısı

	x, y, w, h float64
	whiteW     float64
}

func newKeyboard() *keyboard {
	kb := &keyboard{}
	kb.setRange(60, spanFull) // C4'ten iki oktav
	return kb
}

// setRange görünen tuş aralığını değiştirir. base bir do olmalıdır, yoksa
// siyah/beyaz düzeni kayar.
func (kb *keyboard) setRange(base, span int) {
	kb.base = base - ((base%12)+12)%12 // en yakın alt do'ya hizala
	kb.span = span

	kb.keys = kb.keys[:0]
	kb.whites = 0
	for _, b := range bindings {
		if b.offset > span {
			break
		}
		black := isBlack(b.offset)
		if !black {
			kb.whites++
		}
		kb.keys = append(kb.keys, pianoKey{
			offset: b.offset,
			black:  black,
			label:  b.label,
			key:    b.key,
		})
	}
	kb.layout(kb.x, kb.y, kb.w, kb.h)
}

// fitTo klavyeyi bir şarkının nota aralığına oturtur: parça bir oktava
// sığıyorsa şeritler genişlesin diye tek oktav gösterilir.
func (kb *keyboard) fitTo(s *song.Song) {
	lo, hi := 127, 0
	for _, n := range s.Notes {
		if n.Midi < 0 {
			continue
		}
		if n.Midi < lo {
			lo = n.Midi
		}
		if n.Midi > hi {
			hi = n.Midi
		}
	}
	if lo > hi { // notasız şarkı
		kb.setRange(60, spanFull)
		return
	}

	span := spanOctave
	if hi-lo > spanOctave {
		span = spanFull
	}
	// Aralığı ortalayan do'yu seç, sonra parçayı içeri alacak şekilde kaydır.
	base := (lo+hi)/2 - span/2
	base -= ((base%12)+12)%12
	for base > lo {
		base -= 12
	}
	for base+span < hi {
		base += 12
	}
	kb.setRange(clampInt(base, 24, 108-span), span)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (kb *keyboard) midiAt(i int) int { return kb.base + kb.keys[i].offset }

// indexOfMidi bir MIDI notasının hangi tuşa düştüğünü bulur. Nota aralık
// dışındaysa oktav oktav kaydırarak içeri alır; böylece her şarkı çalınabilir.
func (kb *keyboard) indexOfMidi(midi int) int {
	for midi < kb.base {
		midi += 12
	}
	for midi > kb.base+kb.span {
		midi -= 12
	}
	for i := range kb.keys {
		if kb.midiAt(i) == midi {
			return i
		}
	}
	return -1
}

// shiftOctave temel oktavı kaydırır (piyanonun tamamına erişmek için).
func (kb *keyboard) shiftOctave(delta int) {
	next := kb.base + delta*12
	if next < 24 || next+kb.span > 108 {
		return
	}
	kb.base = next
}

// layout tuşların ekran konumlarını hesaplar.
func (kb *keyboard) layout(x, y, w, h float64) {
	kb.x, kb.y, kb.w, kb.h = x, y, w, h
	if kb.whites == 0 || w <= 0 {
		return
	}
	kb.whiteW = w / float64(kb.whites)

	blackW := kb.whiteW * 0.58
	white := 0
	for i := range kb.keys {
		k := &kb.keys[i]
		if k.black {
			// Siyah tuş, solundaki beyaz tuşun sağ kenarına oturur.
			k.w = blackW
			k.x = x + float64(white)*kb.whiteW - blackW/2
		} else {
			k.w = kb.whiteW
			k.x = x + float64(white)*kb.whiteW
			white++
		}
	}
}

func (kb *keyboard) update(dt float64) {
	for i := range kb.keys {
		k := &kb.keys[i]
		if k.held {
			k.glow = 1
			continue
		}
		k.glow -= dt * 2.6
		if k.glow < 0 {
			k.glow = 0
		}
	}
}

// hit tuşu görsel olarak yakar (ses ayrı çalınır).
func (kb *keyboard) hit(i int) {
	if i >= 0 && i < len(kb.keys) {
		kb.keys[i].glow = 1
	}
}

// centerOf tuşun yatay orta noktasını verir (efektleri konumlandırmak için).
func (kb *keyboard) centerOf(i int) float64 {
	k := kb.keys[i]
	return k.x + k.w/2
}

func (kb *keyboard) draw(dst *ebiten.Image) {
	blackH := kb.h * 0.62

	// Klavyenin üstündeki ince neon çizgi: isabet hattı ile piyanoyu birleştirir.
	fillRounded(dst, float32(kb.x), float32(kb.y-3), float32(kb.w), 6, 3, alpha(colNeon, 0.85))
	drawGlow(dst, kb.x+kb.w/2, kb.y, kb.w*0.5, colNeon, 0.20)

	// Önce beyaz tuşlar, sonra siyahlar (siyahlar üstte kalmalı).
	for i := range kb.keys {
		if !kb.keys[i].black {
			kb.drawWhite(dst, &kb.keys[i])
		}
	}
	for i := range kb.keys {
		if kb.keys[i].black {
			kb.drawBlack(dst, &kb.keys[i], blackH)
		}
	}
}

func (kb *keyboard) drawWhite(dst *ebiten.Image, k *pianoKey) {
	const gap = 2.0
	x := float32(k.x + gap/2)
	w := float32(k.w - gap)
	y := float32(kb.y)
	h := float32(kb.h)

	top := colWhite
	bottom := rgb(0xC9D6F5)
	if k.glow > 0 {
		top = mix(top, colNeonSoft, k.glow*0.85)
		bottom = mix(bottom, colNeon, k.glow)
	}

	fillRounded(dst, x, y, w, h, 10, bottom)
	fillVGradient(dst, x+2, y+2, w-4, h-4, top, bottom)

	if k.glow > 0 {
		drawGlow(dst, float64(x+w/2), kb.y+kb.h*0.25, float64(w)*1.5, colNeon, 0.55*k.glow)
		strokeRounded(dst, x, y, w, h, 10, 2.5, alpha(colNeon, k.glow))
	}

	// Alt kısımda bilgisayar tuşu, onun altında nota adı. Az sayıda tuş
	// gösterildiğinde tuşlar çok genişlediği için yazı boyutu sınırlanır.
	lblSize := clamp(kb.whiteW*0.30, 13, 30)
	drawText(dst, k.label, face(lblSize, true), float64(x)+float64(w)/2, kb.y+kb.h-lblSize-18,
		mix(rgb(0x5B6B99), colInk, k.glow), text.AlignCenter)

	nmSize := clamp(kb.whiteW*0.20, 10, 19)
	drawText(dst, noteName(kb.base+k.offset), face(nmSize, false), float64(x)+float64(w)/2, kb.y+kb.h-16,
		alpha(rgb(0x8391BC), 0.9), text.AlignCenter)
}

func (kb *keyboard) drawBlack(dst *ebiten.Image, k *pianoKey, h float64) {
	x := float32(k.x)
	w := float32(k.w)
	y := float32(kb.y)

	top := rgb(0x1B2450)
	bottom := colBlackKey
	if k.glow > 0 {
		top = mix(top, colNeon, k.glow)
		bottom = mix(bottom, rgb(0x0B4C6B), k.glow)
	}

	// Hafif bir gölge, tuşun beyazların üstünde durduğunu hissettirir.
	fillRounded(dst, x-2, y, w+4, float32(h)+4, 9, alpha(color.NRGBA{A: 255}, 0.28))
	fillRounded(dst, x, y, w, float32(h), 8, bottom)
	fillVGradient(dst, x+2, y+2, w-4, float32(h)*0.6, top, bottom)
	strokeRounded(dst, x, y, w, float32(h), 8, 1.5, alpha(colNeon, 0.35+0.65*k.glow))

	if k.glow > 0 {
		drawGlow(dst, float64(x+w/2), float64(y)+h*0.35, float64(w)*1.8, colNeon, 0.6*k.glow)
	}

	lblSize := clamp(kb.whiteW*0.24, 12, 26)
	drawText(dst, k.label, face(lblSize, true), float64(x+w/2), kb.y+h-lblSize*0.9,
		mix(rgb(0x9FB0DC), colWhite, k.glow), text.AlignCenter)
}
