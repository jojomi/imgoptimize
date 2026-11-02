package optimizer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// encodeTestWebP erzeugt mit den übergebenen magick-Argumenten ein Quellbild,
// kodiert es nach WebP und gibt Quell- und Zielpfad zurück.
func encodeTestWebP(t *testing.T, sourceArgs ...string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("magick not installed")
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "src.png")
	output := filepath.Join(dir, "out.webp")
	if out, err := exec.Command("magick", append(sourceArgs, input)...).CombinedOutput(); err != nil {
		t.Fatalf("creating test image: %v: %s", err, out)
	}

	opt := New(Config{InputPath: input, OutputPath: output, Quality: 80, Silent: true})
	if err := opt.resizeImageWithDimensions(input, output, ""); err != nil {
		t.Fatalf("encode: %v", err)
	}

	return input, output
}

// pixelDiff liefert die Anzahl abweichender Pixel zwischen a und b.
func pixelDiff(t *testing.T, a, b string) string {
	t.Helper()
	// compare beendet sich bei Unterschieden mit Exit-Code 1, daher wird der
	// Fehler ignoriert und nur die Ausgabe ausgewertet.
	out, _ := exec.Command("magick", "compare", "-metric", "AE", a, b, "null:").CombinedOutput()
	return strings.Fields(string(out))[0]
}

// Flächige Grafiken werden verlustfrei kleiner als verlustbehaftet und müssen
// daher pixelgenau bleiben.
func TestWebPFlatGraphicIsEncodedLossless(t *testing.T) {
	input, output := encodeTestWebP(t, "-size", "400x300", "xc:white",
		"-fill", "#1a73e8", "-draw", "rectangle 20,20 380,80",
		"-fill", "#e53935", "-draw", "circle 120,200 170,200",
		// WebP speichert 8 Bit pro Kanal; eine 16-Bit-Quelle würde gerundet.
		"-depth", "8")

	if diff := pixelDiff(t, input, output); diff != "0" {
		t.Fatalf("flat graphic differs in %s pixels, want lossless", diff)
	}
}

// Fotoähnliche Bilder sind verlustbehaftet deutlich kleiner.
func TestWebPPhotoIsEncodedLossy(t *testing.T) {
	input, output := encodeTestWebP(t, "-seed", "1", "-size", "400x300", "plasma:fractal")

	if diff := pixelDiff(t, input, output); diff == "0" {
		t.Fatal("photo-like image encoded lossless, want lossy")
	}
}

// Die verlustfreie Kandidatendatei darf nicht liegen bleiben.
func TestWebPLeavesNoTempFile(t *testing.T) {
	_, output := encodeTestWebP(t, "-size", "64x64", "xc:blue")

	entries, err := os.ReadDir(filepath.Dir(output))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "src.png" && e.Name() != "out.webp" {
			t.Fatalf("unexpected leftover file %s", e.Name())
		}
	}
}
