package game

import (
	"fmt"
	"image/color"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
	path   string
	title  string
	tempo  int
	notes  int
	length float64 // saniye
	stars  int     // 1..5 zorluk
	bad    int     // çözülemeyen nota sayısı
	broken bool    // dosya okunamadı
	best   int     // en iyi skor (diske kaydedilir)
}

// difficulty şarkının nota yoğunluğundan 1..5 arası zorluk çıkarır.
func difficulty(s *song.Song) int {
	d := s.Duration()
	if d <= 0 {
		return 1
	}
	nps := float64(len(s.Notes)) / d
	return int(clamp(float64(1+int(nps/1.2)), 1, 5))
}

// FindSongsDir şarkı klasörünü bulur: önce çalışma klasörüne, sonra
// çalıştırılabilir dosyanın yanına bakar. Böylece exe başka yerden de açılabilir.
func FindSongsDir() string {
	if st, err := os.Stat("songs"); err == nil && st.IsDir() {
		return "songs"
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "songs")
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir
		}
	}
	return "songs"
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
	scroll  float64 // görünen kaydırma (satır cinsinden, yumuşatılmış)
	scrollTarget float64

	cur    *song.Song
	now    float64 // şarkı zamanı; negatifken geri sayım sürer
	paused bool
	auto   bool
	result stats

	resultAuto bool // sonuç otomatik çalmayla alındı
	newBest    bool // sonuç yeni rekor

	save     saveData // kalıcı skorlar ve gecikme ayarı
	songsDir string
	lastTick time.Time

	notice  string // kısa süre görünen bilgi mesajı
	noticeT float64
	lastNote  string // son çalınan notanın adı
	lastNoteT float64

	canvas *ebiten.Image // ekran sarsıntısı için ara tuval

	clock        float64 // toplam süre (arka plan animasyonları için)
	windowFitted bool
}

func (g *Game) offset() float64 { return float64(g.save.OffsetMs) / 1000 }

func (g *Game) adjustOffset(deltaMs int) {
	g.save.OffsetMs = clampInt(g.save.OffsetMs+deltaMs, -300, 300)
	writeSave(g.save)
	g.tell(fmt.Sprintf("Gecikme ayarı %+d ms", g.save.OffsetMs))
}

// importDropped pencereye sürüklenen .mid/.txt dosyalarını şarkı klasörüne
// kopyalar ve listeyi yeniler.
func (g *Game) importDropped() {
	d := ebiten.DroppedFiles()
	if d == nil || g.state == statePlay {
		return
	}
	if err := os.MkdirAll(g.songsDir, 0o755); err != nil {
		g.tell("Şarkı klasörü oluşturulamadı")
		return
	}
	var added []string
	_ = fs.WalkDir(d, ".", func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".mid", ".midi", ".txt":
		default:
			return nil
		}
		data, err := fs.ReadFile(d, p)
		if err != nil {
			return nil
		}
		dst := filepath.Join(g.songsDir, filepath.Base(p))
		if err := os.WriteFile(dst, data, 0o644); err == nil {
			added = append(added, filepath.Base(p))
		}
		return nil
	})
	if len(added) == 0 {
		g.tell("Desteklenen dosya yok (.mid, .txt)")
		return
	}
	g.entries = discover(g.songsDir, g.save.Scores)
	for i, e := range g.entries {
		if filepath.Base(e.path) == added[len(added)-1] {
			g.sel = i
		}
	}
	g.state = stateSelect
	g.kb.setRange(60, spanFull)
	g.tell(fmt.Sprintf("%d şarkı eklendi", len(added)))
}

// tell ekranın üstünde kısa bir bilgi mesajı gösterir.
func (g *Game) tell(msg string) {
	g.notice, g.noticeT = msg, 2.6
}

// New şarkı klasörünü tarayarak oyunu hazırlar.
func New(songsDir string) *Game {
	g := &Game{
		kb:    newKeyboard(),
		hw:    newHighway(),
		fx:    newEffects(),
		state: stateFree,
	}
	g.save = loadSave()
	g.songsDir = songsDir
	g.entries = discover(songsDir, g.save.Scores)
	// İlk karede girdi gelirse efektlerin doğru yerde doğması için başlangıç
	// yerleşimini şimdiden hesapla.
	g.w, g.h = WindowW, WindowH
	g.relayout()
	return g
}

// discover klasördeki .txt ve .mid şarkılarını okur ve başlıklarıyla listeler.
func discover(dir string, scores map[string]int) []entry {
	var matches []string
	for _, pat := range []string{"*.txt", "*.mid", "*.midi"} {
		m, _ := filepath.Glob(filepath.Join(dir, pat))
		matches = append(matches, m...)
	}
	sort.Strings(matches)

	var out []entry
	for _, path := range matches {
		e := entry{path: path, title: filepath.Base(path), best: scores[filepath.Base(path)]}
		if s, err := song.Load(path); err == nil {
			e.title = s.Title
			e.tempo = s.Tempo
			e.notes = len(s.Notes)
			e.length = s.Duration()
			e.stars = difficulty(s)
			e.bad = s.Bad
		} else {
			e.broken = true
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
	// Gerçek geçen süre: sabit 1/60 varsaymak kare düşünce notaların müzikten
	// kaymasına yol açıyordu. Uzun takılmalar (pencere sürükleme) kırpılır.
	now := time.Now()
	dt := 1.0 / 60.0
	if !g.lastTick.IsZero() {
		dt = clamp(now.Sub(g.lastTick).Seconds(), 0.001, 0.05)
	}
	g.lastTick = now
	g.clock += dt
	g.fitWindowOnce()

	g.importDropped()
	g.noticeT -= dt
	g.lastNoteT -= dt
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

	// Oktav kaydırma yalnız şarkı dışında çalışır: şarkı sırasında klavye
	// aralığı notalara sabitlenmiştir, kaydırmak sesleri yanlış perdeye taşırdı.
	if g.state == stateFree {
		if inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft) {
			g.kb.shiftOctave(-1)
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyArrowRight) {
			g.kb.shiftOctave(1)
		}
	}

	// Giriş gecikmesi telafisi: nota hattan önce/sonra geliyormuş gibi
	// hissediliyorsa [ ve ] ile 10 ms'lik adımlarla ayarlanır.
	if inpututil.IsKeyJustPressed(ebiten.KeyBracketLeft) {
		g.adjustOffset(-10)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyBracketRight) {
		g.adjustOffset(10)
	}

	if inpututil.IsKeyJustPressed(ebiten.KeyF11) {
		ebiten.SetFullscreen(!ebiten.IsFullscreen())
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyMinus) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadSubtract) {
		g.tell(fmt.Sprintf("Ses %d%%", int(math.Round(audio.SetVolume(audio.Volume()-0.1)*100))))
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEqual) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadAdd) {
		g.tell(fmt.Sprintf("Ses %d%%", int(math.Round(audio.SetVolume(audio.Volume()+0.1)*100))))
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
	g.lastNote, g.lastNoteT = noteName(g.kb.midiAt(i)), 1.6

	cx := g.kb.centerOf(i)
	line := g.hw.lineY()

	switch g.state {
	case statePlay:
		if !g.paused && g.now >= -winGood {
			if idx, ok := g.hw.press(i, g.now-g.offset()); ok {
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

func (g *Game) startSong() {
	if len(g.entries) == 0 {
		return
	}
	s, err := song.Load(g.entries[g.sel].path)
	if err != nil {
		g.state = stateSelect
		g.tell("Şarkı açılamadı: " + filepath.Base(g.entries[g.sel].path))
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
		g.resultAuto = g.auto
		// Otomatik çalma skoru rekor sayılmaz.
		if e := &g.entries[g.sel]; !g.auto && g.result.score > e.best {
			e.best = g.result.score
			g.newBest = true
			g.save.Scores[filepath.Base(e.path)] = e.best
			writeSave(g.save)
		} else {
			g.newBest = false
		}
		g.state = stateResult
	}
}

// ------------------------------------------------------------------- çizim

func (g *Game) Draw(screen *ebiten.Image) {
	g.w = float64(screen.Bounds().Dx())
	g.h = float64(screen.Bounds().Dy())
	g.relayout()

	dx, dy := g.fx.offset()
	if dx == 0 && dy == 0 {
		g.drawScene(screen)
		return
	}
	// Sarsıntı: sahneyi ara tuvale çizip kaydırarak ekrana bas.
	if g.canvas == nil || g.canvas.Bounds().Size() != screen.Bounds().Size() {
		g.canvas = ebiten.NewImage(screen.Bounds().Dx(), screen.Bounds().Dy())
	}
	g.canvas.Clear()
	g.drawScene(g.canvas)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(dx, dy)
	screen.DrawImage(g.canvas, op)
}

func (g *Game) drawScene(screen *ebiten.Image) {
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
	g.drawNotice(screen)
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
		g.drawHint(screen, pad, y+26, g.w-2*pad,
			"TAB şarkılar   ← → oktav   - + ses   F11 tam ekran   ESC çıkış",
			"TAB şarkılar   ← → oktav   ESC çıkış")
	case stateSelect:
		drawTextGlow(screen, "ŞARKI SEÇ", face(30, true), pad, y, colWhite, text.AlignStart)
		g.drawHint(screen, pad, y+26, g.w-2*pad,
			"← → ↑ ↓ seç   ENTER başlat   F otomatik   [ ] gecikme   TAB serbest   ESC geri",
			"← → ↑ ↓ seç   ENTER başlat   TAB serbest")
	case statePlay, stateResult:
		title := "—"
		if g.cur != nil {
			title = g.cur.Title
		}
		drawTextGlow(screen, title, face(28, true), pad, y, colWhite, text.AlignStart)
		// Sağ üstteki skor alanına çarpmamak için ipucuna kalan genişlik verilir.
		g.drawHint(screen, pad, y+26, g.w-2*pad-230,
			"BOŞLUK duraklat   F5 baştan   F otomatik   ESC listeye dön",
			"BOŞLUK duraklat   F5 baştan   ESC geri")
	}

	// Sağ üst: skor ve isabet oranı (yalnız şarkı modunda).
	if g.state == statePlay || g.state == stateResult {
		st := g.hw.stats
		drawTextGlow(screen, fmt.Sprintf("%d", st.score), face(38, true), g.w-pad, y-6, colWhite, text.AlignEnd)
		drawText(screen, fmt.Sprintf("%.0f%% isabet · kombo %d", st.accuracy()*100, st.combo),
			face(17, false), g.w-pad, y+26, alpha(colWhite, 0.85), text.AlignEnd)
	}

	if g.auto && g.state != stateFree {
		drawText(screen, "OTOMATİK", face(16, true), g.w/2, 26, alpha(colGood, 0.95), text.AlignCenter)
	}

	if g.state == statePlay && g.cur != nil {
		g.drawProgress(screen)
	}
}

// drawProgress pencerenin en üstüne ince bir şarkı ilerleme çubuğu çizer.
func (g *Game) drawProgress(screen *ebiten.Image) {
	x, w, y := 0.0, g.w, 0.0
	frac := 0.0
	if g.hw.total > 0 {
		frac = clamp(g.now/g.hw.total, 0, 1)
	}
	fillRounded(screen, float32(x), float32(y), float32(w), 5, 0, alpha(colWhite, 0.18))
	if frac > 0 {
		fillRounded(screen, float32(x), float32(y), float32(w*frac), 5, 0, colNeon)
		drawGlow(screen, x+w*frac, y+2, 16, colNeon, 0.6)
	}
}

// drawHint ipucu satırını çizer; uzun sürüm sığmazsa kısa sürüme düşer.
func (g *Game) drawHint(screen *ebiten.Image, x, y, maxW float64, long, short string) {
	f := face(17, false)
	s := long
	if w, _ := text.Measure(s, f, 0); w > maxW {
		s = short
	}
	drawText(screen, s, f, x, y, alpha(colWhite, 0.82), text.AlignStart)
}

// drawNotice ses ayarı gibi kısa bilgi mesajlarını üst ortada gösterir.
func (g *Game) drawNotice(screen *ebiten.Image) {
	if g.noticeT <= 0 || g.notice == "" {
		return
	}
	a := clamp(g.noticeT*2, 0, 1)
	f := face(20, true)
	tw, _ := text.Measure(g.notice, f, 0)
	w, h := tw+44, 40.0
	x, y := g.w/2-w/2, g.hw.y+10
	fillRounded(screen, float32(x), float32(y), float32(w), float32(h), 20, alpha(colLane, 0.92*a))
	strokeRounded(screen, float32(x), float32(y), float32(w), float32(h), 20, 1.5, alpha(colNeon, 0.8*a))
	drawText(screen, g.notice, f, g.w/2, y+h/2, alpha(colWhite, a), text.AlignCenter)
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

	// Son çalınan notanın adı: yumuşakça sönen büyük bir gösterge.
	if g.lastNoteT > 0 && g.lastNote != "" {
		a := clamp(g.lastNoteT*1.2, 0, 1)
		drawTextGlow(screen, g.lastNote, face(84, true), cx, cy-110, alpha(colNeon, a), text.AlignCenter)
	}
}

// fmtDuration saniyeyi "1:05" biçimine çevirir.
func fmtDuration(sec float64) string {
	n := int(math.Round(sec))
	return fmt.Sprintf("%d:%02d", n/60, n%60)
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
	// Küçük pencerelerde kart başlığa taşmasın diye ölçeklenir.
	sc := clamp(math.Min((g.hw.h-10)/420, (g.w-40)/520), 0.55, 1)
	cx := g.w / 2
	cy := g.hw.y + g.hw.h*0.5
	w, h := 520.0*sc, 420.0*sc
	top := cy - h/2
	f := func(size float64, bold bool) text.Face { return face(size*sc, bold) }

	// Arkadaki şerit ve notaları karart; kart öne çıksın.
	fillRounded(screen, float32(g.hw.x), float32(g.hw.y), float32(g.hw.w), float32(g.hw.h), 4, alpha(colInk, 0.55))
	fillRounded(screen, float32(cx-w/2), float32(top), float32(w), float32(h), 26, alpha(colLane, 0.97))
	strokeRounded(screen, float32(cx-w/2), float32(top), float32(w), float32(h), 26, 2.5, alpha(colNeon, 0.9))

	rank, rankColor := g.result.rank()
	drawText(screen, "SONUÇ", f(20, true), cx, top+36*sc, alpha(colWhite, 0.7), text.AlignCenter)
	drawTextGlow(screen, rank, f(96, true), cx, top+112*sc, rankColor, text.AlignCenter)
	drawTextGlow(screen, fmt.Sprintf("%d", g.result.score), f(46, true), cx, top+200*sc, colWhite, text.AlignCenter)

	switch {
	case g.newBest:
		drawText(screen, "YENİ REKOR!", f(18, true), cx, top+238*sc, colGood, text.AlignCenter)
	case g.resultAuto:
		drawText(screen, "Otomatik çalma — rekor sayılmaz", f(16, false), cx, top+238*sc,
			alpha(colWhite, 0.6), text.AlignCenter)
	}

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

	y := top + 276*sc
	for _, r := range rows {
		drawText(screen, r.label, f(19, false), cx-w/2+40*sc, y, alpha(colWhite, 0.85), text.AlignStart)
		drawText(screen, strconv.Itoa(r.value), f(19, true), cx+w/2-40*sc, y, r.clr, text.AlignEnd)
		y += 28 * sc
	}

	drawText(screen, "F5 baştan   ·   ESC listeye dön", f(18, false), cx, top+h-24*sc,
		alpha(colNeonSoft, 0.9), text.AlignCenter)
}
