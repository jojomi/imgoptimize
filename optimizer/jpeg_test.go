package optimizer

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// JPEGs werden immer progressiv und mit 4:2:0-Farbunterabtastung gespeichert,
// auch bei hoher Qualität, wo ImageMagick und mozjpeg sonst die volle
// Farbauflösung wählen und die Datei deutlich größer wird.
func TestJPEGIsProgressiveWith420Subsampling(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("magick not installed")
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "src.png")
	if out, err := exec.Command("magick", "-seed", "1", "-size", "128x128", "plasma:", input).CombinedOutput(); err != nil {
		t.Fatalf("creating test image: %v: %s", err, out)
	}

	t.Run("mozjpeg", func(t *testing.T) {
		if New(Config{}).mozjpegPath() == "" {
			t.Skip("mozjpeg not installed")
		}
		assertProgressive420(t, input, filepath.Join(dir, "moz.jpg"))
	})

	t.Run("imagemagick", func(t *testing.T) {
		saved := mozjpegCandidates
		mozjpegCandidates = nil
		t.Cleanup(func() { mozjpegCandidates = saved })

		assertProgressive420(t, input, filepath.Join(dir, "magick.jpg"))
	})
}

func assertProgressive420(t *testing.T, input, output string) {
	t.Helper()

	opt := New(Config{InputPath: input, OutputPath: output, Quality: 95, Silent: true})
	if err := opt.resizeImageWithDimensions(input, output, ""); err != nil {
		t.Fatalf("encode: %v", err)
	}

	out, err := exec.Command("magick", "identify", "-format", "%[jpeg:sampling-factor] %[interlace]", output).Output()
	if err != nil {
		t.Fatalf("identify: %v", err)
	}
	if got, want := strings.TrimSpace(string(out)), "2x2,1x1,1x1 JPEG"; got != want {
		t.Fatalf("sampling/interlace = %q, want %q", got, want)
	}
}
