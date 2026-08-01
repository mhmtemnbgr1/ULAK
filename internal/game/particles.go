package game

import (
	"image/color"
	"math"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
	text "github.com/hajimehoshi/ebiten/v2/text/v2"
)

// ripple bir tuşa basıldığında isabet hattından dışa açılan halka.
type ripple struct {
	x, y  float64
	life  float64 // 1 -> 0
	scale float64
	clr   color.NRGBA
}

// spark isabet anında savrulan küçük kıvılcım.
type spark struct {
	x, y   float64
	vx, vy float64
	life   float64
	size   float64
	clr    color.NRGBA
}

// riser serbest çalma modunda tuştan yukarı doğru süzülen neon şerit.
type riser struct {
	x, w float64
	y    float64
	life float64
	clr  color.NRGBA
}

// popup "Mükemmel!" gibi kısa süre görünen yazı.
type popup struct {
	x, y float64
	txt  string
	life float64
	clr  color.NRGBA
}

// bokeh arka planda yavaşça süzülen ışık lekesi; sahneye derinlik katar.
type bokeh struct {
	x, y   float64
	r      float64
	speed  float64
	phase  float64
	bright float64
}

// effects tüm görsel efektleri tek yerden yönetir.
type effects struct {
	ripples []ripple
	sparks  []spark
	risers  []riser
	popups  []popup
	bokehs  []bokeh

	shake float64 // ekran sarsıntısı (büyük komboda)
}

func newEffects() *effects {
	e := &effects{}
	for i := 0; i < 18; i++ {
		e.bokehs = append(e.bokehs, bokeh{
			x:      rand.Float64(),
			y:      rand.Float64(),
			r:      40 + rand.Float64()*130,
			speed:  0.006 + rand.Float64()*0.02,
			phase:  rand.Float64() * math.Pi * 2,
			bright: 0.05 + rand.Float64()*0.10,
		})
	}
	return e
}

// ---------------------------------------------------------------- tetikleyici

// burst bir tuş isabetinde halka + kıvılcım üretir.
func (e *effects) burst(x, y float64, clr color.NRGBA, power float64) {
	e.ripples = append(e.ripples, ripple{x: x, y: y, life: 1, scale: 22 + 16*power, clr: clr})

	n := int(6 + 10*power)
	for i := 0; i < n; i++ {
		a := -math.Pi/2 + (rand.Float64()-0.5)*2.2
		sp := 120 + rand.Float64()*260*power
		e.sparks = append(e.sparks, spark{
			x: x, y: y,
			vx:   math.Cos(a) * sp,
			vy:   math.Sin(a) * sp,
			life: 0.45 + rand.Float64()*0.5,
			size: 2 + rand.Float64()*3,
			clr:  clr,
		})
	}
}

// rise serbest çalma modunda tuştan yukarı çıkan şerit bırakır.
func (e *effects) rise(x, w, y float64, clr color.NRGBA) {
	e.risers = append(e.risers, riser{x: x, w: w, y: y, life: 1, clr: clr})
}

func (e *effects) say(x, y float64, txt string, clr color.NRGBA) {
	e.popups = append(e.popups, popup{x: x, y: y, txt: txt, life: 1, clr: clr})
}

func (e *effects) kick(amount float64) {
	if amount > e.shake {
		e.shake = amount
	}
}

// -------------------------------------------------------------------- döngü

func (e *effects) update(dt float64) {
	e.ripples = filter(e.ripples, func(r *ripple) bool {
		r.life -= dt * 1.8
		return r.life > 0
	})
	e.sparks = filter(e.sparks, func(s *spark) bool {
		s.life -= dt
		s.x += s.vx * dt
		s.y += s.vy * dt
		s.vy += 620 * dt // yerçekimi
		s.vx *= 1 - 1.4*dt
		return s.life > 0
	})
	e.risers = filter(e.risers, func(r *riser) bool {
		r.life -= dt * 1.15
		r.y -= 420 * dt
		return r.life > 0
	})
	e.popups = filter(e.popups, func(p *popup) bool {
		p.life -= dt * 1.3
		p.y -= 42 * dt
		return p.life > 0
	})

	for i := range e.bokehs {
		b := &e.bokehs[i]
		b.y -= b.speed * dt
		if b.y < -0.2 {
			b.y = 1.2
			b.x = rand.Float64()
		}
		b.phase += dt * 0.6
	}

	e.shake -= dt * 26
	if e.shake < 0 {
		e.shake = 0
	}
}

// filter yerinde süzme yapar; her karede yeniden dilim ayırmayı önler.
func filter[T any](s []T, keep func(*T) bool) []T {
	out := s[:0]
	for i := range s {
		if keep(&s[i]) {
			out = append(out, s[i])
		}
	}
	return out
}

// ------------------------------------------------------------------- çizim

// drawBokeh arka plandaki yumuşak ışık leklerini çizer (en altta kalır).
func (e *effects) drawBokeh(dst *ebiten.Image, w, h float64) {
	for _, b := range e.bokehs {
		pulse := 0.75 + 0.25*math.Sin(b.phase)
		drawGlow(dst, b.x*w, b.y*h, b.r*pulse, colWhite, b.bright*pulse)
	}
}

// drawRisers serbest modun yükselen şeritleri; notaların arkasında kalmalı.
func (e *effects) drawRisers(dst *ebiten.Image) {
	for _, r := range e.risers {
		a := easeOut(r.life)
		// Yükseldikçe uzayıp incelen bir ışık izi.
		hgt := 60 + 260*(1-r.life)
		wid := r.w * (0.42 * r.life)
		cx := r.x + r.w/2
		fillRounded(dst, float32(cx-wid/2), float32(r.y-hgt), float32(wid), float32(hgt),
			float32(wid/2), alpha(r.clr, 0.22*a))
		drawGlow(dst, cx, r.y-hgt*0.35, wid*2.2, r.clr, 0.18*a)
	}
}

// drawFront halka, kıvılcım ve yazılar; her şeyin üstüne çizilir.
func (e *effects) drawFront(dst *ebiten.Image) {
	for _, r := range e.ripples {
		t := 1 - r.life
		rad := float32(r.scale * (0.35 + 1.5*easeOut(t)))
		strokeCircle(dst, float32(r.x), float32(r.y), rad, 3, alpha(r.clr, 0.55*r.life))
		drawGlow(dst, r.x, r.y, float64(rad)*1.4, r.clr, 0.30*r.life)
	}
	for _, s := range e.sparks {
		a := clamp(s.life*1.6, 0, 1)
		drawGlow(dst, s.x, s.y, s.size*3.2, s.clr, 0.55*a)
		fillRounded(dst, float32(s.x-s.size/2), float32(s.y-s.size/2),
			float32(s.size), float32(s.size), float32(s.size/2), alpha(colWhite, a))
	}
	for _, p := range e.popups {
		a := clamp(p.life*1.5, 0, 1)
		f := face(26+10*(1-p.life), true)
		drawTextGlow(dst, p.txt, f, p.x, p.y, alpha(p.clr, a), text.AlignCenter)
	}
}

// offset ekran sarsıntısının o karedeki kaymasını verir.
func (e *effects) offset() (float64, float64) {
	if e.shake <= 0 {
		return 0, 0
	}
	return (rand.Float64() - 0.5) * e.shake, (rand.Float64() - 0.5) * e.shake
}
