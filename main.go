// Neon Piyano: Go ile yazılmış, klavyeden çalınabilen grafik bir piyano.
// Serbest çalma modunun yanında, yukarıdan düşen notalara zamanında basılan
// bir ritim modu içerir.
package main

import (
	"fmt"
	"os"

	"github.com/hajimehoshi/ebiten/v2"

	"piano/internal/audio"
	"piano/internal/game"
)

func main() {
	if err := audio.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "Ses başlatılamadı:", err)
		os.Exit(1)
	}

	ebiten.SetWindowSize(game.WindowW, game.WindowH)
	ebiten.SetWindowTitle("Neon Piyano")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetVsyncEnabled(true)

	if err := ebiten.RunGame(game.New("songs")); err != nil {
		fmt.Fprintln(os.Stderr, "Hata:", err)
		os.Exit(1)
	}
}
