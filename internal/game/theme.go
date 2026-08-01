// Package game Ebitengine tabanlı grafik arayüzü içerir: neon görünümlü bir
// piyano, yukarıdan düşen notalarla ritim modu ve serbest çalma modu.
package game

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	text "github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// ---------------------------------------------------------------- renk paleti

// Arayüzün tamamı bu paletten beslenir: pembe→mor→camgöbeği bir arka plan
// gradyanı, üzerinde koyu lacivert şeritler ve camgöbeği neon vurgular.
var (
	colBgPink   = rgb(0xFF7FD8)
	colBgViolet = rgb(0x8B5CF6)
	colBgCyan   = rgb(0x38E0F0)

	colLane       = rgb(0x0A0F2C) // düşen nota şeritlerinin zemini
	colLaneEdge   = rgb(0xFFFFFF)
	colTileTop    = rgb(0x14265E) // nota bloğunun üst rengi
	colTileBottom = rgb(0x050A22)
	colNeon       = rgb(0x35E7FF) // ana vurgu (camgöbeği)
	colNeonSoft   = rgb(0x8AF3FF)
	colWhite      = rgb(0xFFFFFF)
	colBlackKey   = rgb(0x0B1030)
	colInk        = rgb(0x120A2A) // açık zemin üstündeki yazı

	colPerfect = rgb(0x4DF3B0)
	colGood    = rgb(0xFFD166)
	colMiss    = rgb(0xFF6B9D)
)

func rgb(hex uint32) color.NRGBA {
	return color.NRGBA{R: uint8(hex >> 16), G: uint8(hex >> 8), B: uint8(hex), A: 0xFF}
}

// alpha rengin saydamlığını 0..1 aralığında ayarlar.
func alpha(c color.NRGBA, a float64) color.NRGBA {
	c.A = uint8(clamp(a, 0, 1) * 255)
	return c
}

// mix iki rengi t=0..1 oranında karıştırır.
func mix(a, b color.NRGBA, t float64) color.NRGBA {
	t = clamp(t, 0, 1)
	return color.NRGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: uint8(float64(a.A) + (float64(b.A)-float64(a.A))*t),
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// easeOut yumuşak yavaşlayan animasyon eğrisi (efektlerin sönümü için).
func easeOut(t float64) float64 { return 1 - math.Pow(1-clamp(t, 0, 1), 3) }

// -------------------------------------------------------------------- dokular

var (
	texOnce  sync.Once
	texGlow  *ebiten.Image // yumuşak radyal parlama
	texBg    *ebiten.Image // arka plan gradyanı (küçük, ekrana esnetilir)
	texPixel *ebiten.Image // 1x1 beyaz
)

func initTextures() {
	texOnce.Do(func() {
		texGlow = makeGlow(128)
		texBg = makeBackdrop(96)
		texPixel = ebiten.NewImage(1, 1)
		texPixel.Fill(color.White)
	})
}

// makeGlow merkezden dışa doğru sönen yumuşak bir daire üretir. Neon
// parlamaları bu dokuyu ölçekleyip renklendirerek çizilir.
func makeGlow(size int) *ebiten.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	c := float64(size-1) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := math.Hypot(float64(x)-c, float64(y)-c) / c
			a := clamp(1-d, 0, 1)
			a = a * a * a // kenarlara doğru hızlı sönüm
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: uint8(a * 255)})
		}
	}
	return ebiten.NewImageFromImage(img)
}

// makeBackdrop köşegen boyunca pembe→mor→camgöbeği geçen arka planı üretir.
func makeBackdrop(size int) *ebiten.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Köşegen konum: sol üst 0, sağ alt 1.
			t := (float64(x)/float64(size-1)*0.55 + float64(y)/float64(size-1)*0.45)
			var c color.NRGBA
			if t < 0.5 {
				c = mix(colBgPink, colBgViolet, t/0.5)
			} else {
				c = mix(colBgViolet, colBgCyan, (t-0.5)/0.5)
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return ebiten.NewImageFromImage(img)
}

// --------------------------------------------------------------------- çizim

// drawGlow verilen merkeze, yarıçapa ve renge sahip yumuşak bir ışık lekesi
// çizer. Toplamalı harmanlama sayesinde üst üste binince parlaklık artar.
func drawGlow(dst *ebiten.Image, cx, cy, radius float64, clr color.NRGBA, strength float64) {
	initTextures()
	s := radius * 2 / float64(texGlow.Bounds().Dx())
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(s, s)
	op.GeoM.Translate(cx-radius, cy-radius)
	op.ColorScale.ScaleWithColor(alpha(clr, strength))
	op.Blend = ebiten.BlendLighter
	dst.DrawImage(texGlow, op)
}

// roundedRect yuvarlatılmış köşeli bir dikdörtgen yolu oluşturur.
func roundedRect(p *vector.Path, x, y, w, h, r float32) {
	if r > w/2 {
		r = w / 2
	}
	if r > h/2 {
		r = h / 2
	}
	if r < 0 {
		r = 0
	}
	p.MoveTo(x+r, y)
	p.LineTo(x+w-r, y)
	p.ArcTo(x+w, y, x+w, y+r, r)
	p.LineTo(x+w, y+h-r)
	p.ArcTo(x+w, y+h, x+w-r, y+h, r)
	p.LineTo(x+r, y+h)
	p.ArcTo(x, y+h, x, y+h-r, r)
	p.LineTo(x, y+r)
	p.ArcTo(x, y, x+r, y, r)
	p.Close()
}

func fillRounded(dst *ebiten.Image, x, y, w, h, r float32, clr color.NRGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	var p vector.Path
	roundedRect(&p, x, y, w, h, r)
	op := &vector.DrawPathOptions{AntiAlias: true}
	op.ColorScale.ScaleWithColor(clr)
	vector.FillPath(dst, &p, nil, op)
}

func strokeRounded(dst *ebiten.Image, x, y, w, h, r, width float32, clr color.NRGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	var p vector.Path
	roundedRect(&p, x, y, w, h, r)
	op := &vector.DrawPathOptions{AntiAlias: true}
	op.ColorScale.ScaleWithColor(clr)
	vector.StrokePath(dst, &p, &vector.StrokeOptions{Width: width, LineJoin: vector.LineJoinRound}, op)
}

// fillVGradient dikey renk geçişli bir dikdörtgen çizer (ince şeritlerle).
func fillVGradient(dst *ebiten.Image, x, y, w, h float32, top, bottom color.NRGBA) {
	const steps = 24
	if h <= 0 || w <= 0 {
		return
	}
	sh := h / steps
	for i := 0; i < steps; i++ {
		c := mix(top, bottom, float64(i)/(steps-1))
		vector.DrawFilledRect(dst, x, y+float32(i)*sh, w, sh+1, c, false)
	}
}

// strokeCircle içi boş bir halka çizer (uzun nota göstergesi, isabet dalgası).
func strokeCircle(dst *ebiten.Image, cx, cy, r, width float32, clr color.NRGBA) {
	if r <= 0 {
		return
	}
	var p vector.Path
	p.Arc(cx, cy, r, 0, 2*math.Pi, vector.Clockwise)
	p.Close()
	op := &vector.DrawPathOptions{AntiAlias: true}
	op.ColorScale.ScaleWithColor(clr)
	vector.StrokePath(dst, &p, &vector.StrokeOptions{Width: width, LineJoin: vector.LineJoinRound}, op)
}

// --------------------------------------------------------------------- yazı

var (
	fontOnce    sync.Once
	fontRegular *text.GoTextFaceSource
	fontBold    *text.GoTextFaceSource
	faceCache   = map[faceKey]text.Face{}
	faceMu      sync.Mutex
)

type faceKey struct {
	size float64
	bold bool
}

func initFonts() {
	fontOnce.Do(func() {
		var err error
		if fontRegular, err = text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF)); err != nil {
			panic(err)
		}
		if fontBold, err = text.NewGoTextFaceSource(bytes.NewReader(gobold.TTF)); err != nil {
			panic(err)
		}
	})
}

// face istenen boyutta bir yazı tipi döndürür; boyutlar önbelleklenir.
func face(size float64, bold bool) text.Face {
	initFonts()
	// Animasyonlu boyutların önbelleği şişirmemesi için tam piksele yuvarla.
	size = math.Round(size)
	if size < 6 {
		size = 6
	}

	faceMu.Lock()
	defer faceMu.Unlock()

	k := faceKey{size, bold}
	if f, ok := faceCache[k]; ok {
		return f
	}
	src := fontRegular
	if bold {
		src = fontBold
	}
	f := &text.GoTextFace{Source: src, Size: size}
	faceCache[k] = f
	return f
}

// drawText metni (x, y) noktasına, yatayda align'e göre ve dikeyde ortalanmış
// olarak çizer.
func drawText(dst *ebiten.Image, s string, f text.Face, x, y float64, clr color.NRGBA, align text.Align) {
	op := &text.DrawOptions{}
	op.GeoM.Translate(x, y)
	op.ColorScale.ScaleWithColor(clr)
	op.PrimaryAlign = align
	op.SecondaryAlign = text.AlignCenter
	text.Draw(dst, s, f, op)
}

// drawTextGlow metni önce yumuşak bir hâle, sonra net olarak çizer; başlıklara
// neon bir parlaklık verir.
func drawTextGlow(dst *ebiten.Image, s string, f text.Face, x, y float64, clr color.NRGBA, align text.Align) {
	w, _ := text.Measure(s, f, 0)
	drawGlow(dst, x+offsetForAlign(w, align), y, w*0.55+24, clr, 0.30)
	drawText(dst, s, f, x, y, clr, align)
}

func offsetForAlign(w float64, align text.Align) float64 {
	switch align {
	case text.AlignCenter:
		return 0
	case text.AlignEnd:
		return -w / 2
	default:
		return w / 2
	}
}
