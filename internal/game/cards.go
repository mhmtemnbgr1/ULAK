package game

import (
	"fmt"
	"image/color"
	"image"
	"math"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	text "github.com/hajimehoshi/ebiten/v2/text/v2"
)

// Şarkı kartı ızgarasının ölçüleri.
const (
	cardMinW = 270.0
	cardMaxW = 340.0
	cardH    = 176.0
	cardGap  = 18.0
)

// gridCols pencere genişliğine göre sütun sayısını verir.
func (g *Game) gridCols() int {
	n := int((g.hw.w - 2*cardGap + cardGap) / (cardMinW + cardGap))
	if n < 1 {
		n = 1
	}
	if n > len(g.entries) && len(g.entries) > 0 {
		n = len(g.entries)
	}
	return n
}

// gridRows görünen satır sayısıdır (en az bir).
func (g *Game) gridRows() int {
	n := int((g.hw.h - cardGap) / (cardH + cardGap))
	if n < 1 {
		n = 1
	}
	return n
}

// cardRect i. kartın (kaydırma dahil) ekran dikdörtgenidir.
func (g *Game) cardRect(i int) (x, y, w, h float64) {
	cols := g.gridCols()
	w = math.Min((g.hw.w-2*cardGap-float64(cols-1)*cardGap)/float64(cols), cardMaxW)
	total := float64(cols)*w + float64(cols-1)*cardGap
	x0 := g.hw.x + (g.hw.w-total)/2
	row, col := i/cols, i%cols
	x = x0 + float64(col)*(w+cardGap)
	y = g.hw.y + cardGap + (float64(row)-g.scroll)*(cardH+cardGap)
	return x, y, w, cardH
}

func (g *Game) updateSelect() {
	n := len(g.entries)
	if n == 0 {
		return
	}
	cols := g.gridCols()

	move := func(d int) {
		g.sel = clampInt(g.sel+d, 0, n-1)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowRight) {
		move(1)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft) {
		move(-1)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowDown) {
		move(cols)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowUp) {
		move(-cols)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter) {
		g.startSong()
		return
	}

	// Fare: karta tıkla seç, seçili karta tekrar tıkla başlat.
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		mx, my := ebiten.CursorPosition()
		for i := range g.entries {
			x, y, w, h := g.cardRect(i)
			if float64(mx) >= x && float64(mx) <= x+w && float64(my) >= y && float64(my) <= y+h &&
				float64(my) >= g.hw.y && float64(my) <= g.hw.y+g.hw.h {
				if i == g.sel {
					g.startSong()
					return
				}
				g.sel = i
				break
			}
		}
	}
	if _, wy := ebiten.Wheel(); wy != 0 {
		move(-int(math.Copysign(1, wy)) * cols)
	}

	// Seçili satır görünür kalsın, kaydırma yumuşakça akar.
	rows := g.gridRows()
	selRow := float64(g.sel / cols)
	target := g.scrollTarget
	if selRow < target {
		target = selRow
	}
	if selRow > target+float64(rows-1) {
		target = selRow - float64(rows-1)
	}
	g.scrollTarget = target
	g.scroll += (target - g.scroll) * 0.2
}

// cardAccent karta sıra numarasına göre palette bir vurgu rengi verir.
func cardAccent(i int) (a, b colorPair) {
	pal := []colorPair{
		{colBgPink, colBgViolet},
		{colBgViolet, colBgCyan},
		{colBgCyan, colPerfect},
		{colGood, colBgPink},
	}
	return pal[i%len(pal)], pal[(i+1)%len(pal)]
}

// truncate metni yazı tipine göre maxW genişliğe sığdırır, taşarsa "…" koyar.
func truncate(s string, f text.Face, maxW float64) string {
	if w, _ := text.Measure(s, f, 0); w <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 1 {
		r = r[:len(r)-1]
		t := strings.TrimSpace(string(r)) + "…"
		if w, _ := text.Measure(t, f, 0); w <= maxW {
			return t
		}
	}
	return s
}

// drawSelect şarkıları kapaklı kartlar halinde ızgarada çizer.
func (g *Game) drawSelect(screen *ebiten.Image) {
	if len(g.entries) == 0 {
		drawText(screen, "songs/ klasöründe şarkı yok. .mid ya da .txt dosyasını pencereye sürükle.", face(22, true),
			g.hw.x+g.hw.w/2, g.hw.y+g.hw.h/2, colWhite, text.AlignCenter)
		return
	}

	// Çizim şerit alanına kırpılır; kaydırırken kartlar başlığa taşmaz.
	clip := image.Rect(int(g.hw.x), int(g.hw.y), int(g.hw.x+g.hw.w), int(g.hw.y+g.hw.h))
	dst := screen.SubImage(clip.Intersect(screen.Bounds())).(*ebiten.Image)

	for i, e := range g.entries {
		x, y, w, h := g.cardRect(i)
		if y+h < g.hw.y || y > g.hw.y+g.hw.h {
			continue
		}
		selected := i == g.sel
		fx, fy, fw, fh := float32(x), float32(y), float32(w), float32(h)

		body := alpha(colLane, 0.88)
		if selected {
			body = alpha(rgb(0x14265E), 0.96)
			drawGlow(dst, x+w/2, y+h/2, w*0.75, colNeon, 0.20)
		}
		fillRounded(dst, fx, fy, fw, fh, 18, body)

		// Kapak: köşegen gradyan şerit + büyük sıra numarası.
		ca, cb := cardAccent(i)
		const coverH = 64.0
		fillRounded(dst, fx+6, fy+6, fw-12, coverH, 13, ca.a)
		fillRounded(dst, fx+6+(fw-12)*0.55, fy+6, (fw-12)*0.45, coverH, 13, alpha(cb.a, 0.55))
		drawText(dst, fmt.Sprintf("%02d", i+1), face(34, true), x+22, y+6+coverH/2, alpha(colWhite, 0.95), text.AlignStart)
		if isMIDI(e.path) {
			bw := 56.0
			fillRounded(dst, fx+fw-float32(bw)-18, fy+6+float32(coverH)/2-12, float32(bw), 24, 12, alpha(colInk, 0.55))
			drawText(dst, "MIDI", face(14, true), x+w-bw/2-18, y+6+coverH/2, colWhite, text.AlignCenter)
		}

		if selected {
			strokeRounded(dst, fx, fy, fw, fh, 18, 2.5, colNeon)
		} else {
			strokeRounded(dst, fx, fy, fw, fh, 18, 1.5, alpha(colNeonSoft, 0.25))
		}

		tx := x + 20
		drawText(dst, truncate(e.title, face(21, true), w-40), face(21, true), tx, y+coverH+30, colWhite, text.AlignStart)

		info := fmt.Sprintf("%s · %d BPM · %d nota", fmtDuration(e.length), e.tempo, e.notes)
		switch {
		case e.broken:
			info = "Dosya okunamadı"
		case e.bad > 0:
			info = fmt.Sprintf("%d geçersiz nota", e.bad)
		}
		drawText(dst, truncate(info, face(15, false), w-40), face(15, false), tx, y+coverH+56,
			alpha(colNeonSoft, 0.9), text.AlignStart)

		// Alt satır: zorluk noktaları ve en iyi skor.
		for d := 0; d < 5; d++ {
			dc := alpha(colWhite, 0.18)
			if d < e.stars {
				dc = colGood
			}
			fillRounded(dst, float32(tx)+float32(d)*15, float32(y+h-26), 9, 9, 4.5, dc)
		}
		best := "—"
		if e.best > 0 {
			best = fmt.Sprintf("%d", e.best)
		}
		drawText(dst, "en iyi "+best, face(15, false), x+w-20, y+h-22, alpha(colWhite, 0.8), text.AlignEnd)
	}

	// Taşan satırlar için kenar göstergesi: üstte/altta daha fazla kart var.
	cols := g.gridCols()
	lastRow := (len(g.entries) - 1) / cols
	if g.scroll > 0.3 {
		drawText(dst, "▲", face(16, true), g.hw.x+g.hw.w/2, g.hw.y+12, alpha(colNeonSoft, 0.8), text.AlignCenter)
	}
	if float64(lastRow)-g.scroll > float64(g.gridRows())-0.7 {
		drawText(dst, "▼", face(16, true), g.hw.x+g.hw.w/2, g.hw.y+g.hw.h-12, alpha(colNeonSoft, 0.8), text.AlignCenter)
	}
}

func isMIDI(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mid", ".midi":
		return true
	}
	return false
}

// colorPair iki renkli kapak paleti girdisidir.
type colorPair struct{ a, b color.NRGBA }
