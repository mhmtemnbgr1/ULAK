package game

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// saveData diske yazılan kalıcı verilerdir: şarkı başına en iyi skor ve
// kullanıcının ayarladığı giriş gecikmesi telafisi.
type saveData struct {
	Scores   map[string]int `json:"scores"`
	OffsetMs int            `json:"offset_ms"`
}

// saveFile kayıt dosyasının yoludur. Kullanıcı yapılandırma klasörü
// bulunamazsa boş döner ve veriler yalnız bellekte kalır.
func saveFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "ULAK", "save.json")
}

func loadSave() saveData {
	d := saveData{Scores: map[string]int{}}
	if path := saveFile(); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &d)
		}
	}
	if d.Scores == nil {
		d.Scores = map[string]int{}
	}
	return d
}

// writeSave kayıt dosyasını yazar; hata oyunu durdurmamalıdır.
func writeSave(d saveData) {
	path := saveFile()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if data, err := json.MarshalIndent(d, "", "  "); err == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}
