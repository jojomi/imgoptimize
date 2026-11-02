package optimizer

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Persönliche Metadaten (Kamera, GPS, Urheber) müssen in jedem Ausgabeformat
// entfernt sein, ohne dass ein separates Werkzeug wie mat2 nötig ist.
func TestMetadataIsAlwaysStripped(t *testing.T) {
	for _, tool := range []string{"magick", "exiftool"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "src.jpg")
	if out, err := exec.Command("magick", "-size", "64x64", "plasma:", input).CombinedOutput(); err != nil {
		t.Fatalf("creating test image: %v: %s", err, out)
	}
	tag := exec.Command("exiftool", "-q", "-overwrite_original",
		"-Make=TestCam", "-GPSLatitude=48.1", "-GPSLatitudeRef=N", "-Artist=Secret", "-XMP:Creator=Secret", input)
	if out, err := tag.CombinedOutput(); err != nil {
		t.Fatalf("tagging test image: %v: %s", err, out)
	}

	for _, ext := range []string{"jpg", "png", "webp", "avif"} {
		output := filepath.Join(dir, "out."+ext)
		_, err := OptimizeWithoutResize(Config{InputPath: input, OutputPath: output, Quality: 80, Silent: true})
		if err != nil {
			t.Fatalf("%s: OptimizeWithoutResize: %v", ext, err)
		}

		out, err := exec.Command("exiftool", "-s", "-s", "-s", "-Make", "-GPSLatitude", "-Artist", "-Creator", output).Output()
		if err != nil {
			t.Fatalf("%s: reading metadata: %v", ext, err)
		}
		if found := strings.TrimSpace(string(out)); found != "" {
			t.Errorf("%s still contains metadata: %q", ext, found)
		}
	}
}
