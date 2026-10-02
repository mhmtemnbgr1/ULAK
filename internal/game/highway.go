package game

import (
	"image"
	"image/color"
	"strconv"

	"github.com/hajimehoshi/ebiten/v2"
	text "github.com/hajimehoshi/ebiten/v2/text/v2"

	"piano/internal/song"
)

// İsabet pencereleri (saniye). Dışına taşan nota kaçmış sayılır.
const (
	winPerfect = 0.09
	winGood    = 0.20
)

type grade int

const (
	gradeNone grade = iota
	gradePerfect
	gradeGood
	gradeMiss
)

func (g grade) label() string {
	switch g {
	case gradePerfect:
		return "MÜKEMMEL"
	case gradeGood:
		return "İYİ"
	case gradeMiss:
		return "KAÇTI"
	}
	return ""
}

func (g grade) color() color.NRGBA {
	switch g {
	case gradePerfect:
		return colPerfect
	case gradeGood:
		return colGood
	case gradeMiss:
		return colMiss
	}
	return colWhite
}

// tile yukarıdan düşen tek bir nota bloğudur.
type tile struct {
	keyIdx int     // klavyedeki tuş sırası
	freq   float64 // çalınacak frekans
	start  float64 // isabet anı (şarkı başından itibaren saniye)
	dur    float64
	judged bool
	grade  grade
	flash  float64 // isabetten sonraki kısa parlama
}

// stats bir şarkı denemesinin skor tablosudur.
type stats struct {
	score    int
	combo    int
	maxCombo int
	perfect  int
	good     int
	miss     int
}

func (s stats) total() int { return s.perfect + s.good + s.miss }

// accuracy 0..1 aralığında isabet oranı (mükemmeller tam, iyiler yarım sayar).
func (s stats) accuracy() float64 {
	if s.total() == 0 {
		return 0
	}
	return (float64(s.perfect) + 0.6*float64(s.good)) / float64(s.total())
}

// rank isabet oranını harfe çevirir.
func (s stats) rank() (string, color.NRGBA) {
	switch a := s.accuracy(); {
	case s.total() == 0:
		return "-", colWhite
	case a >= 0.95:
		return "S", colPerfect
	case a >= 0.85:
		return "A", colNeon
	case a >= 0.70:
		return "B", colGood
	case a >= 0.50:
		return "C", colBgPink
	default:
		return "D", colMiss
	}
}

// highway notaların düştüğü alandır; alt kenarı isabet hattıdır.
type highway struct {
	x, y, w, h float64
	lead       float64 // bir notanın tepeden hatta inmesi (saniye)

	tiles []tile
	stats stats
	total float64 // şarkının bitiş anı (saniye)

	lastGrade grade
	pulse     float64 // isabet hattının parlama miktarı
}

func newHighway() *highway {
	return &highway{lead: 2.1}
}

// load bir şarkıyı düşen notalara çevirir. Aralık dışı kalan notalar klavyeye
// sığacak şekilde oktav kaydırılır.
func (hw *highway) load(s *song.Song, kb *keyboard) {
	hw.tiles = hw.tiles[:0]
	hw.stats = stats{}
	hw.lastGrade = gradeNone
	defer func() { hw.total = hw.duration() }()

	for _, n := range s.Notes {
		if n.Midi < 0 {
			continue
		}
		if idx := kb.indexOfMidi(n.Midi); idx >= 0 {
			hw.tiles = append(hw.tiles, tile{
				keyIdx: idx,
				freq:   n.Freq,
				start:  n.Start,
				dur:    n.Seconds,
			})
		}
	}
}

// duration şarkının son notasının bittiği anı verir.
func (hw *highway) duration() float64 {
	var end float64
	for _, t := range hw.tiles {
		if e := t.start + t.dur; e > end {
			end = e
		}
	}
	return end
}

func (hw *highway) layout(x, y, w, h float64) {
	hw.x, hw.y, hw.w, hw.h = x, y, w, h
}

// lineY isabet hattının ekran yüksekliğidir.
func (hw *highway) lineY() float64 { return hw.y + hw.h }

// pixelsPerSecond notaların düşme hızıdır.
func (hw *highway) pps() float64 { return hw.h / hw.lead }

// update kaçan notaları işaretler ve parlamaları söndürür.
func (hw *highway) update(now, dt float64) []int {
	var missed []int
	for i := range hw.tiles {
		t := &hw.tiles[i]
		if !t.judged && now > t.start+winGood {
			t.judged = true
			t.grade = gradeMiss
			hw.stats.miss++
			hw.stats.combo = 0
			hw.lastGrade = gradeMiss
			missed = append(missed, i)
		}
		if t.flash > 0 {
			t.flash -= dt * 2.2
		}
	}
	if hw.pulse > 0 {
		hw.pulse -= dt * 3
	}
	return missed
}

// press bir tuşa basıldığında o şeritteki en yakın notayı değerlendirir.
// Değerlendirilecek nota yoksa ikinci dönüş değeri false olur.
func (hw *highway) press(keyIdx int, now float64) (int, bool) {
	best, bestDT := -1, winGood
	for i := range hw.tiles {
		t := &hw.tiles[i]
		if t.judged || t.keyIdx != keyIdx {
			continue
		}
		dt := t.start - now
		if dt < 0 {
			dt = -dt
		}
		if dt <= bestDT {
			best, bestDT = i, dt
		}
	}
	if best < 0 {
		return -1, false
	}

	hw.judge(best, bestDT)
	return best, true
}

// judge bir notayı zamanlama hatasına göre puanlar.
func (hw *highway) judge(i int, offset float64) {
	t := &hw.tiles[i]
	t.judged = true
	t.flash = 1
	if offset <= winPerfect {
		t.grade = gradePerfect
		hw.stats.perfect++
		hw.stats.score += 300 + hw.stats.combo*5
	} else {
		t.grade = gradeGood
		hw.stats.good++
		hw.stats.score += 150 + hw.stats.combo*2
	}
	hw.stats.combo++
	if hw.stats.combo > hw.stats.maxCombo {
		hw.stats.maxCombo = hw.stats.combo
	}
	hw.lastGrade = t.grade
	hw.pulse = 1
}

// dueTiles otomatik çalma modunda hattı geçen notaları döndürür.
func (hw *highway) dueTiles(now float64) []int {
	var due []int
	for i := range hw.tiles {
		if !hw.tiles[i].judged && now >= hw.tiles[i].start {
			due = append(due, i)
		}
	}
	return due
}

// finished tüm notalar değerlendirildiğinde true olur.
func (hw *highway) finished(now float64) bool {
	return now > hw.duration()+winGood+0.4
}

// ------------------------------------------------------------------- çizim

// drawLanes notaların düştüğü koyu şeritleri çizer. Şeritler beyaz tuşlarla
// hizalıdır, böylece düşen nota ile basılacak tuş göz tarafından eşleşir.
func (hw *highway) drawLanes(dst *ebiten.Image, kb *keyboard, active bool) {
	body := alpha(colLane, 0.90)
	if !active {
		body = alpha(colLane, 0.74)
	}
	for i := 0; i < kb.whites; i++ {
		x := float32(hw.x + float64(i)*kb.whiteW)
		fillRounded(dst, x+1, float32(hw.y), float32(kb.whiteW)-2, float32(hw.h), 4, body)
		// Şerit ayracı: referanstaki beyaz ince çizgiler.
		fillRounded(dst, x-1, float32(hw.y), 2, float32(hw.h), 1, alpha(colLaneEdge, 0.16))
	}
	right := float32(hw.x + float64(kb.whites)*kb.whiteW)
	fillRounded(dst, right-1, float32(hw.y), 2, float32(hw.h), 1, alpha(colLaneEdge, 0.16))
}

// drawTiles düşen notaları çizer. Çizim şerit alanına kırpılır, böylece
// yukarıdan gelen notalar başlığın üstüne taşmaz.
func (hw *highway) drawTiles(screen *ebiten.Image, kb *keyboard, now float64) {
	pps := hw.pps()
	line := hw.lineY()

	clip := image.Rect(int(hw.x), int(hw.y), int(hw.x+hw.w), int(line))
	if !clip.Overlaps(screen.Bounds()) {
		return
	}
	dst := screen.SubImage(clip.Intersect(screen.Bounds())).(*ebiten.Image)

	for i := range hw.tiles {
		t := &hw.tiles[i]
		if t.judged && t.flash <= 0 {
			continue
		}

		bottom := line - (t.start-now)*pps
		height := t.dur * pps
		if height < 34 {
			height = 34
		}
		top := bottom - height

		// Alan dışındakileri atla; kalanı kırpma zaten hallediyor.
		if top > line || bottom < hw.y {
			continue
		}

		k := kb.keys[t.keyIdx]
		x := float32(k.x + 3)
		w := float32(k.w - 6)
		y := float32(top)
		h := float32(bottom - top)

		edge := colNeon
		if k.black {
			edge = colBgPink // siyah tuş notaları farklı renkte, ayırt edilsin
		}

		// Gövde: koyu lacivert blok, üstten alta doğru koyulaşan gradyan.
		fillRounded(dst, x, y, w, h, 10, colTileBottom)
		fillVGradient(dst, x+2, y+2, w-4, h-4, colTileTop, colTileBottom)
		strokeRounded(dst, x, y, w, h, 10, 2, alpha(edge, 0.9))
		drawGlow(dst, float64(x+w/2), float64(y+h/2), float64(w)*0.9, edge, 0.16)
		// Üst kenarda parlak bir çizgi: bloğa hacim verir.
		fillRounded(dst, x+6, y+3, w-12, 3, 1.5, alpha(colNeonSoft, 0.75))

		// Notanın basılacağı bilgisayar tuşu: kısa notada ortada, uzunda üstte.
		lblSize := clamp(float64(w)*0.34, 12, 26)
		lblY := float64(y + h/2)
		if h > 92 {
			lblY = float64(y) + 8 + lblSize*0.8
		}
		drawText(dst, k.label, face(lblSize, true), float64(x+w/2), lblY, alpha(colWhite, 0.92), text.AlignCenter)

		// Uzun notalarda referanstaki beyaz çizgi + halka göstergesi.
		if h > 92 {
			cx := float64(x + w/2)
			ringY := float64(y+h) - 26
			topY := lblY + lblSize*0.8
			drawVLine(dst, cx, topY, ringY-12, 3, alpha(colWhite, 0.92))
			strokeCircle(dst, float32(cx), float32(ringY), float32(w)*0.19, 3, alpha(colWhite, 0.92))
			drawGlow(dst, cx, ringY, float64(w)*0.5, colNeonSoft, 0.35)
		}

		// İsabet edilince blok kısa süre beyaza patlar.
		if t.flash > 0 {
			fillRounded(dst, x, y, w, h, 10, alpha(colWhite, 0.55*t.flash))
		}
	}
}

// drawJudgeLine isabet hattını ve son değerlendirmenin parlamasını çizer.
func (hw *highway) drawJudgeLine(dst *ebiten.Image) {
	y := hw.lineY()
	fillRounded(dst, float32(hw.x), float32(y-2), float32(hw.w), 4, 2, alpha(colWhite, 0.45))
	if hw.pulse > 0 {
		c := hw.lastGrade.color()
		fillRounded(dst, float32(hw.x), float32(y-3), float32(hw.w), 6, 3, alpha(c, 0.8*hw.pulse))
		drawGlow(dst, hw.x+hw.w/2, y, hw.w*0.45, c, 0.28*hw.pulse)
	}
}

// drawCombo kombo sayacını şeritlerin ortasında soluk bir filigran olarak
// çizer. Notaların üstünü kapatmaması için notalardan önce ve düşük
// saydamlıkla çizilir.
func (hw *highway) drawCombo(dst *ebiten.Image) {
	if hw.stats.combo < 3 {
		return
	}
	cx := hw.x + hw.w/2
	cy := hw.y + hw.h*0.40

	drawText(dst, strconv.Itoa(hw.stats.combo), face(150, true), cx, cy,
		alpha(colWhite, 0.16), text.AlignCenter)
	drawText(dst, "KOMBO", face(28, true), cx, cy+92, alpha(colNeonSoft, 0.22), text.AlignCenter)
}

// drawVLine x ekseninde ortalanmış, yumuşak uçlu dikey bir çizgi çizer.
func drawVLine(dst *ebiten.Image, x, y0, y1, w float64, clr color.NRGBA) {
	fillRounded(dst, float32(x-w/2), float32(y0), float32(w), float32(y1-y0), float32(w/2), clr)
}
