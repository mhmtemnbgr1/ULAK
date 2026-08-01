// Package audio hoparlörü başlatır ve nota frekanslarından piyano benzeri
// sesler sentezler. Hazır ses dosyası kullanmaz; her tonu gerçek zamanlı üretir.
package audio

import (
	"math"
	"sync"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/speaker"
)

const sampleRate beep.SampleRate = 44100

var (
	initOnce sync.Once
	mixer    = &beep.Mixer{}
)

// Init hoparlörü açar. Program başında bir kez çağrılmalı.
func Init() error {
	var err error
	initOnce.Do(func() {
		// ~1/10 sn tampon: düşük gecikme, canlı çalma için yeterli.
		err = speaker.Init(sampleRate, sampleRate.N(100e6))
		speaker.Play(mixer)
	})
	return err
}

// Play verilen frekansta, verilen süre (saniye) boyunca çalan bir nota tetikler.
// Çağrı hemen döner; ses arka planda mixer üzerinde çalınır.
func Play(freq, seconds float64) {
	speaker.Lock()
	mixer.Add(newTone(freq, seconds))
	speaker.Unlock()
}

// tone tek bir piyano notasını üreten beep.Streamer'ıdır.
type tone struct {
	freq  float64
	pos   int // üretilen örnek sayısı
	total int // toplam örnek sayısı
}

func newTone(freq, seconds float64) *tone {
	return &tone{
		freq:  freq,
		total: int(seconds * float64(sampleRate)),
	}
}

func (t *tone) Stream(samples [][2]float64) (n int, ok bool) {
	sr := float64(sampleRate)
	dur := float64(t.total) / sr

	for i := range samples {
		if t.pos >= t.total {
			return i, i > 0
		}
		time := float64(t.pos) / sr

		// Üstel sönümlenen zarf: hızlı çıkış, yumuşak iniş (piyano tuşu hissi).
		env := math.Exp(-3.5 * time / dur)
		// Çok kısa bir attack ile "tık" sesini engelle.
		if attack := 0.005; time < attack {
			env *= time / attack
		}

		// Temel ton + harmonikler → daha zengin, piyanomsu tını.
		ph := 2 * math.Pi * t.freq * time
		v := math.Sin(ph) +
			0.5*math.Sin(2*ph) +
			0.25*math.Sin(3*ph) +
			0.12*math.Sin(4*ph)
		v *= env * 0.18 // genel ses seviyesi (kırpılmayı önler)

		samples[i][0] = v
		samples[i][1] = v
		t.pos++
		n++
	}
	return n, true
}

func (t *tone) Err() error { return nil }
