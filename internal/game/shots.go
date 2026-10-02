package game

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"

	"piano/internal/audio"
)

// ShotRunner uygulamayı betikle sürüp README için ekran görüntüleri alır.
// Gerçek arayüz çizilir; yalnızca girdi yerine oyun durumu doğrudan ayarlanır.
// `go run . -docs docs` ile çalıştırılır.
type ShotRunner struct {
	g      *Game
	dir    string
	frame  int
	step   int
	wait   int // adımlar arası bekleme kareleri
	err    error
	pending string // bir sonraki Draw'da kaydedilecek dosya adı
}

// NewShotRunner görüntülerin yazılacağı klasörü hazırlar.
func NewShotRunner(g *Game, dir string) (*ShotRunner, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	g.windowFitted = true // pencereyi küçültme; çıktı her makinede aynı olsun
	audio.SetVolume(0)
	return &ShotRunner{g: g, dir: dir}, nil
}

func (r *ShotRunner) Layout(int, int) (int, int) { return WindowW, WindowH }

func (r *ShotRunner) Update() error {
	if r.err != nil {
		return r.err
	}
	if err := r.g.Update(); err != nil {
		return err
	}
	r.frame++
	if r.wait > 0 {
		r.wait--
		return nil
	}
	g := r.g

	switch r.step {
	case 0: // serbest çalma: bir akor ve üst oktavdan bir nota
		for _, i := range []int{0, 4, 7, 12, 16} {
			g.kb.hit(i)
			g.playKey(i)
		}
		r.next(14, "")
	case 1:
		r.next(0, "serbest.png")
		r.wait = 6
	case 2: // şarkı listesi
		g.state = stateSelect
		g.sel = 1
		g.scroll = 1
		r.next(30, "")
	case 3:
		r.next(0, "sarkilar.png")
		r.wait = 6
	case 4: // şarkı: otomatik modda notalar düşerken
		g.sel = 0
		g.auto = true
		g.startSong()
		r.next(0, "")
	case 5:
		if g.now < 6.5 {
			return nil
		}
		r.next(0, "sarki.png")
		r.wait = 6
	case 6: // sonuç: şarkının sonuna atla, otomatik mod hepsini isabetler
		g.now = g.hw.total - 0.6
		r.next(0, "")
	case 7:
		if g.state != stateResult {
			return nil
		}
		r.next(40, "")
	case 8:
		r.next(0, "sonuc.png")
		r.wait = 6
	default:
		return ebiten.Termination
	}
	return nil
}

func (r *ShotRunner) next(wait int, shot string) {
	r.step++
	r.wait = wait
	r.pending = shot
}

func (r *ShotRunner) Draw(screen *ebiten.Image) {
	r.g.Draw(screen)
	if r.pending == "" {
		return
	}
	name := r.pending
	r.pending = ""

	b := screen.Bounds()
	img := image.NewRGBA(b)
	screen.ReadPixels(img.Pix)
	f, err := os.Create(filepath.Join(r.dir, name))
	if err != nil {
		r.err = err
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		r.err = errors.Join(err, fmt.Errorf("%s yazılamadı", name))
	}
}
