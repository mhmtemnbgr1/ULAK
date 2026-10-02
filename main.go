// Neon Piyano: Go ile yazılmış, klavyeden çalınabilen grafik bir piyano.
// Serbest çalma modunun yanında, yukarıdan düşen notalara zamanında basılan
// bir ritim modu içerir.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hajimehoshi/ebiten/v2"

	"piano/internal/audio"
	"piano/internal/game"
)

func main() {
	docs := flag.String("docs", "", "README görsellerini bu klasöre üretip çık")
	flag.Parse()

	if err := audio.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "Ses başlatılamadı:", err)
		os.Exit(1)
	}

	ebiten.SetWindowSize(game.WindowW, game.WindowH)
	ebiten.SetWindowTitle("Neon Piyano")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetVsyncEnabled(true)

	var run ebiten.Game = game.New(game.FindSongsDir())
	if *docs != "" {
		sr, err := game.NewShotRunner(run.(*game.Game), *docs)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Hata:", err)
			os.Exit(1)
		}
		run = sr
	}
	if err := ebiten.RunGame(run); err != nil {
		fmt.Fprintln(os.Stderr, "Hata:", err)
		os.Exit(1)
	}
}
