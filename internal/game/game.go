package game

import (
	"fmt"
	"image/color"
	"math"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	text "github.com/hajimehoshi/ebiten/v2/text/v2"

	"piano/internal/audio"
	"piano/internal/song"
	"piano/internal/theory"
)

// Pencerenin başlangıç ölçüsü.
const (
	WindowW = 1280
	WindowH = 800
)

type state int

const (
	stateFree   state = iota // serbest çalma
	stateSelect              // şarkı seçimi
	statePlay                // şarkı çalınıyor
	stateResult              // sonuç ekranı
)

// entry seçim ekranındaki bir şarkı satırıdır.
type entry struct {
	path  string
	title string
	tempo int
	notes int
	best  int // bu oturumdaki en iyi skor
}

// Game Ebitengine'in çalıştırdığı ana nesnedir.
type Game struct {
	w, h float64

	kb *keyboard
	hw *highway
	fx *effects

	state   state
	entries []entry
	sel     int
	scroll  float64

	cur    *song.Song
	now    float64 // şarkı zamanı; negatifken geri sayım sürer
	paused bool
	auto   bool
	result stats

	clock        float64 // toplam süre (arka plan animasyonları için)
	windowFitted bool
}

// New şarkı klasörünü tarayarak oyunu hazırlar.
func New(songsDir string) *Game {
	g := &Game{
		kb:    newKeyboard(),
		hw:    newHighway(),
		fx:    newEffects(),
		state: stateFree,
	}
	g.entries = discover(songsDir)
	// İlk karede girdi gelirse efektlerin doğru yerde doğması için başlangıç
	// yerleşimini şimdiden hesapla.
	g.w, g.h = WindowW, WindowH
	g.relayout()
	return g
}

// discover klasördeki .txt şarkılarını okur ve başlıklarıyla listeler.
func discover(dir string) []entry {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	sort.Strings(matches)

	var out []entry
	for _, path := range matches {
		e := entry{path: path, title: filepath.Base(path)}
		if s, err := song.Load(path); err == nil {
			e.title = s.Title
			e.tempo = s.Tempo
			e.notes = len(s.Notes)
		}
		out = append(out, e)
	}
	return out
}

// ---------------------------------------------------------------- Ebitengine

func (g *Game) Layout(outW, outH int) (int, int) { return outW, outH }

// fitWindowOnce pencereyi ekrana sığdırıp ortalar. ebiten.Monitor() oyun
// döngüsü başlamadan nil döndüğü için bu ilk karede yapılmalıdır.
func (g *Game) fitWindowOnce() {
	if g.windowFitted {
		return
	}
	m := ebiten.Monitor()
	if m == nil {
		return // monitör henüz hazır değil, sonraki karede dene
	}
	g.windowFitted = true

	mw, mh := m.Size()
	if mw <= 0 || mh <= 0 {
		return
	}
	w, h := WindowW, WindowH
	// Görev çubuğu ve pencere çerçevesi için pay bırak.
	if maxW := mw - 80; w > maxW {
		w = maxW
	}
	if maxH := mh - 120; h > maxH {
		h = maxH
	}
	ebiten.SetWindowSize(w, h)
	ebiten.SetWindowPosition((mw-w)/2, (mh-h)/3)
}

func (g *Game) Update() error {
	const dt = 1.0 / 60.0
	g.clock += dt
	g.fitWindowOnce()

	g.kb.update(dt)
	g.fx.update(dt)

	if err := g.handleGlobalKeys(); err != nil {
		return err
	}
	g.handlePianoKeys()

	switch g.state {
	case stateSelect:
		g.updateSelect()
	case statePlay:
		g.updatePlay(dt)
	case stateResult:
		if inpututil.IsKeyJustPressed(keyRestart) {
			g.startSong()
		}
	}
	return nil
}

// Piyano tuşlarıyla çakışmayan kontroller. Harf tuşlarının çoğu notalara
// bağlı olduğu için baştan başlatma F5'tedir.
const keyRestart = ebiten.KeyF5

// handleGlobalKeys mod geçişleri ve genel kısayolları işler. Serbest moddaki
// ESC oyunu kapatır; bunu Ebiten'e ebiten.Termination ile bildiririz.
func (g *Game) handleGlobalKeys() error {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		switch g.state {
		case stateFree:
			return ebiten.Termination
		case stateSelect:
			g.state = stateFree
		case statePlay, stateResult:
			g.state = stateSelect
			g.kb.setRange(60, spanFull) // şarkıya daraltılan klavyeyi geri aç
		}
	}

	if inpututil.IsKeyJustPressed(ebiten.KeyTab) {
		switch g.state {
		case stateFree:
			g.state = stateSelect
		case stateSelect:
			g.state = stateFree
		}
	}

	// Oktav kaydırma her modda çalışır. Ok tuşları hiçbir notaya bağlı değil.
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft) {
		g.kb.shiftOctave(-1)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowRight) {
		g.kb.shiftOctave(1)
	}

	if inpututil.IsKeyJustPressed(ebiten.KeyF) && (g.state == statePlay || g.state == stateSelect) {
		g.auto = !g.auto
	}
	return nil
}

// handlePianoKeys piyano tuşlarını dinler: ses çalar, efekt üretir ve şarkı
// modunda isabet değerlendirir.
func (g *Game) handlePianoKeys() {
	for i := range g.kb.keys {
		k := &g.kb.keys[i]
		// Tuşun basılı olup olmadığını her karede doğrudan sor. JustReleased
		// ile izlemek, basış ve bırakış aynı kareye düştüğünde tuşu sonsuza
		// dek basılı bırakıyordu.
		k.held = ebiten.IsKeyPressed(k.key)
		if !inpututil.IsKeyJustPressed(k.key) {
			continue
		}
		g.kb.hit(i)
		g.playKey(i)
	}
}

// playKey tek bir tuş basımının ses ve görsel sonucunu üretir.
func (g *Game) playKey(i int) {
	freq := theory.MidiToFreq(g.kb.midiAt(i))
	audio.Play(freq, 1.8)

	cx := g.kb.centerOf(i)
	line := g.hw.lineY()

	switch g.state {
	case statePlay:
		if !g.paused && g.now >= -winGood {
			if idx, ok := g.hw.press(i, g.now); ok {
				t := g.hw.tiles[idx]
				g.fx.burst(cx, line, t.grade.color(), 1.0)
				g.fx.say(cx, line-70, t.grade.label(), t.grade.color())
				if c := g.hw.stats.combo; c%10 == 0 {
					g.fx.kick(9)
					g.fx.say(g.hw.x+g.hw.w/2, g.hw.y+g.hw.h*0.35,
						fmt.Sprintf("%d KOMBO!", c), colNeonSoft)
				}
				return
			}
		}
		// Boşa basış: ses çıkar ama puan yok.
		g.fx.burst(cx, line, alpha(colWhite, 0.6), 0.35)
	default:
		g.fx.burst(cx, line, colNeon, 0.7)
		g.fx.rise(g.kb.keys[i].x, g.kb.keys[i].w, line, colNeonSoft)
	}
}

// ------------------------------------------------------------- şarkı seçimi

func (g *Game) updateSelect() {
	if len(g.entries) == 0 {
		return
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowDown) {
		g.sel = (g.sel + 1) % len(g.entries)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowUp) {
		g.sel = (g.sel - 1 + len(g.entries)) % len(g.entries)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter) {
		g.startSong()
	}

	// Seçili satırı yumuşakça takip et.
	g.scroll += (float64(g.sel) - g.scroll) * 0.18
}

func (g *Game) startSong() {
	s, err := song.Load(g.entries[g.sel].path)
	if err != nil {
		return
	}
	g.cur = s
	// Önce klavyeyi parçanın aralığına oturt: dar parçalarda şeritler genişler
	// ve notalar ekrana yayılır. Nota-şerit eşlemesi buna göre kurulur.
	g.kb.fitTo(s)
	g.hw.load(s, g.kb)
	// Geri sayım kadar önceden başlat: ilk notalar ekrana yukarıdan girsin.
	g.now = -(g.hw.lead + 1.2)
	g.paused = false
	g.state = statePlay
}

// ------------------------------------------------------------ şarkı çalınıyor

func (g *Game) updatePlay(dt float64) {
	if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		g.paused = !g.paused
	}
	if inpututil.IsKeyJustPressed(keyRestart) {
		g.startSong()
		return
	}
	if g.paused {
		return
	}

	g.now += dt

	// Otomatik mod: notaları hattı geçtikleri anda kusursuz çalar.
	if g.auto {
		for _, i := range g.hw.dueTiles(g.now) {
			t := g.hw.tiles[i]
			g.hw.judge(i, 0)
			g.kb.hit(t.keyIdx)
			audio.Play(t.freq, math.Max(t.dur, 0.9))
			g.fx.burst(g.kb.centerOf(t.keyIdx), g.hw.lineY(), colPerfect, 0.8)
		}
	}

	for _, i := range g.hw.update(g.now, dt) {
		t := g.hw.tiles[i]
		g.fx.say(g.kb.centerOf(t.keyIdx), g.hw.lineY()-70, gradeMiss.label(), colMiss)
	}

	if g.hw.finished(g.now) {
		g.result = g.hw.stats
		if g.result.score > g.entries[g.sel].best {
			g.entries[g.sel].best = g.result.score
		}
		g.state = stateResult
	}
}

// ------------------------------------------------------------------- çizim

func (g *Game) Draw(screen *ebiten.Image) {
	g.w = float64(screen.Bounds().Dx())
	g.h = float64(screen.Bounds().Dy())
	g.relayout()

	g.drawBackground(screen)
	g.fx.drawBokeh(screen, g.w, g.h)

	active := g.state == statePlay
	g.hw.drawLanes(screen, g.kb, active)
	g.fx.drawRisers(screen)

	switch g.state {
	case statePlay, stateResult:
		g.hw.drawCombo(screen)
		g.hw.drawTiles(screen, g.kb, g.now)
	case stateFree:
		g.drawFreeHint(screen)
	case stateSelect:
		g.drawSelect(screen)
	}

	g.hw.drawJudgeLine(screen)
	g.kb.draw(screen)
	g.fx.drawFront(screen)

	g.drawHeader(screen)

	if g.state == statePlay && g.now < 0 {
		g.drawCountdown(screen)
	}
	if g.state == statePlay && g.paused {
		g.drawPaused(screen)
	}
	if g.state == stateResult {
		g.drawResult(screen)
	}
}

// relayout pencere boyutuna göre alanları yeniden hesaplar.
func (g *Game) relayout() {
	const header = 78.0
	kbH := clamp(g.h*0.27, 150, 250)
	kbY := g.h - kbH - 14
	pad := 18.0

	g.kb.layout(pad, kbY, g.w-2*pad, kbH)
	g.hw.layout(pad, header, g.w-2*pad, kbY-header-10)
}

func (g *Game) drawBackground(screen *ebiten.Image) {
	initTextures()
	op := &ebiten.DrawImageOptions{}
	b := texBg.Bounds()
	op.GeoM.Scale(g.w/float64(b.Dx()), g.h/float64(b.Dy()))
	op.Filter = ebiten.FilterLinear
	screen.DrawImage(texBg, op)

	// Üstte ve altta hafif koyulaştırma: yazılar her zaman okunur kalsın.
	fillVGradient(screen, 0, 0, float32(g.w), 150, alpha(colInk, 0.45), alpha(colInk, 0))
	fillVGradient(screen, 0, float32(g.h-120), float32(g.w), 120, alpha(colInk, 0), alpha(colInk, 0.35))
}

func (g *Game) drawHeader(screen *ebiten.Image) {
	y := 40.0
	pad := 30.0

	switch g.state {
	case stateFree:
		drawTextGlow(screen, "SERBEST ÇALMA", face(30, true), pad, y, colWhite, text.AlignStart)
		g.drawHint(screen, "TAB şarkılar   ← → oktav   ESC çıkış", pad, y+26)
	case stateSelect:
		drawTextGlow(screen, "ŞARKI SEÇ", face(30, true), pad, y, colWhite, text.AlignStart)
		g.drawHint(screen, "↑ ↓ seç   ENTER başlat   F otomatik   TAB serbest çalma", pad, y+26)
	case statePlay, stateResult:
		title := "—"
		if g.cur != nil {
			title = g.cur.Title
		}
		drawTextGlow(screen, title, face(28, true), pad, y, colWhite, text.AlignStart)
		g.drawHint(screen, "BOŞLUK duraklat   F5 baştan   F otomatik   ESC listeye dön", pad, y+26)
	}

	// Sağ üst: skor ve isabet oranı (yalnız şarkı modunda).
	if g.state == statePlay || g.state == stateResult {
		st := g.hw.stats
		drawTextGlow(screen, fmt.Sprintf("%d", st.score), face(38, true), g.w-pad, y-6, colWhite, text.AlignEnd)
		drawText(screen, fmt.Sprintf("%.0f%% isabet · kombo %d", st.accuracy()*100, st.combo),
			face(17, false), g.w-pad, y+26, alpha(colWhite, 0.85), text.AlignEnd)
	}

	if g.auto {
		drawText(screen, "OTOMATİK", face(16, true), g.w/2, 26, alpha(colGood, 0.95), text.AlignCenter)
	}
}

func (g *Game) drawHint(screen *ebiten.Image, s string, x, y float64) {
	drawText(screen, s, face(17, false), x, y, alpha(colWhite, 0.82), text.AlignStart)
}

// drawFreeHint serbest modda şeritlerin ortasında yumuşak bir yönlendirme.
func (g *Game) drawFreeHint(screen *ebiten.Image) {
	cx := g.hw.x + g.hw.w/2
	cy := g.hw.y + g.hw.h*0.38
	pulse := 0.55 + 0.15*math.Sin(g.clock*2)

	drawTextGlow(screen, "Çalmaya başla", face(46, true), cx, cy, alpha(colWhite, pulse+0.25), text.AlignCenter)
	drawText(screen, "Alt sıra: Z X C V B N M   ·   Üst sıra: Q W E R T Y U I",
		face(20, false), cx, cy+52, alpha(colWhite, 0.75), text.AlignCenter)
	drawText(screen, "Siyah tuşlar: S D G H J   ve   2 3 5 6 7",
		face(20, false), cx, cy+82, alpha(colWhite, 0.7), text.AlignCenter)
	drawText(screen, "TAB ile şarkı listesine geç",
		face(19, false), cx, cy+124, alpha(colNeonSoft, 0.9), text.AlignCenter)
}

// drawSelect şarkı listesini kart kart çizer.
func (g *Game) drawSelect(screen *ebiten.Image) {
	if len(g.entries) == 0 {
		drawText(screen, "songs/ klasöründe .txt şarkı bulunamadı", face(24, true),
			g.hw.x+g.hw.w/2, g.hw.y+g.hw.h/2, colWhite, text.AlignCenter)
		return
	}

	cardW := clamp(g.hw.w*0.62, 420, 760)
	cardH := 82.0
	gap := 14.0
	cx := g.hw.x + g.hw.w/2
	// Seçili kart alanın ortasında dursun.
	cy := g.hw.y + g.hw.h/2

	for i, e := range g.entries {
		dy := (float64(i) - g.scroll) * (cardH + gap)
		y := cy + dy
		if y < g.hw.y-cardH || y > g.hw.y+g.hw.h+cardH {
			continue
		}

		selected := i == g.sel
		// Merkezden uzaklaştıkça kartlar söner; alan kenarına varmadan
		// kaybolsunlar ki klavyenin arkasında sert kesilmesinler.
		fade := clamp(1-math.Abs(dy)/(g.hw.h*0.40), 0, 1)
		if fade <= 0.02 {
			continue
		}

		x := cx - cardW/2
		top := y - cardH/2

		bg := alpha(colLane, 0.85*fade)
		if selected {
			bg = alpha(rgb(0x14265E), 0.95)
		}
		fillRounded(screen, float32(x), float32(top), float32(cardW), float32(cardH), 16, bg)

		if selected {
			strokeRounded(screen, float32(x), float32(top), float32(cardW), float32(cardH), 16, 2.5, colNeon)
			drawGlow(screen, cx, y, cardW*0.55, colNeon, 0.22)
		} else {
			strokeRounded(screen, float32(x), float32(top), float32(cardW), float32(cardH), 16, 1.5,
				alpha(colNeonSoft, 0.25*fade))
		}

		drawText(screen, e.title, face(26, true), x+26, y-12, alpha(colWhite, fade), text.AlignStart)
		info := fmt.Sprintf("%d nota · %d BPM", e.notes, e.tempo)
		if e.best > 0 {
			info += fmt.Sprintf("   ·   en iyi %d", e.best)
		}
		drawText(screen, info, face(17, false), x+26, y+18, alpha(colNeonSoft, 0.85*fade), text.AlignStart)

		if selected {
			drawText(screen, "ENTER", face(20, true), x+cardW-26, y, colNeon, text.AlignEnd)
		}
	}
}

func (g *Game) drawCountdown(screen *ebiten.Image) {
	left := -g.now
	n := int(math.Ceil(left))
	if n <= 0 || n > 3 {
		return
	}
	frac := left - math.Floor(left) // 1 -> 0 arası, sayı değişince sıfırlanır
	scale := 1.4 - 0.4*frac

	cx := g.hw.x + g.hw.w/2
	cy := g.hw.y + g.hw.h*0.42
	drawGlow(screen, cx, cy, 130*scale, colNeon, 0.35*frac)
	drawTextGlow(screen, fmt.Sprintf("%d", n), face(96*scale, true), cx, cy,
		alpha(colWhite, clamp(frac*1.4, 0, 1)), text.AlignCenter)
}

func (g *Game) drawPaused(screen *ebiten.Image) {
	cx := g.hw.x + g.hw.w/2
	cy := g.hw.y + g.hw.h*0.45
	fillRounded(screen, float32(cx-190), float32(cy-58), 380, 116, 20, alpha(colLane, 0.9))
	strokeRounded(screen, float32(cx-190), float32(cy-58), 380, 116, 20, 2, alpha(colNeon, 0.8))
	drawTextGlow(screen, "DURAKLATILDI", face(34, true), cx, cy-14, colWhite, text.AlignCenter)
	drawText(screen, "BOŞLUK ile devam et", face(18, false), cx, cy+22, alpha(colNeonSoft, 0.9), text.AlignCenter)
}

func (g *Game) drawResult(screen *ebiten.Image) {
	cx := g.w / 2
	cy := g.h*0.42 - 20
	w, h := 520.0, 380.0

	fillRounded(screen, float32(cx-w/2), float32(cy-h/2), float32(w), float32(h), 26, alpha(colLane, 0.94))
	strokeRounded(screen, float32(cx-w/2), float32(cy-h/2), float32(w), float32(h), 26, 2.5, alpha(colNeon, 0.9))

	rank, rankColor := g.result.rank()
	drawText(screen, "SONUÇ", face(20, true), cx, cy-h/2+38, alpha(colWhite, 0.7), text.AlignCenter)
	drawTextGlow(screen, rank, face(110, true), cx, cy-h/2+124, rankColor, text.AlignCenter)
	drawTextGlow(screen, fmt.Sprintf("%d", g.result.score), face(46, true), cx, cy-h/2+196, colWhite, text.AlignCenter)

	rows := []struct {
		label string
		value int
		clr   color.NRGBA
	}{
		{"Mükemmel", g.result.perfect, colPerfect},
		{"İyi", g.result.good, colGood},
		{"Kaçan", g.result.miss, colMiss},
		{"En iyi kombo", g.result.maxCombo, colNeonSoft},
	}

	y := cy - h/2 + 244
	for _, r := range rows {
		drawText(screen, r.label, face(19, false), cx-w/2+40, y, alpha(colWhite, 0.85), text.AlignStart)
		drawText(screen, strconv.Itoa(r.value), face(19, true), cx+w/2-40, y, r.clr, text.AlignEnd)
		y += 28
	}

	drawText(screen, "F5 baştan   ·   ESC listeye dön", face(18, false), cx, cy+h/2-26,
		alpha(colNeonSoft, 0.9), text.AlignCenter)
}
