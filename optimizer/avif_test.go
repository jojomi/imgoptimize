package optimizer

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Rote RGB-Werte unter vollständig transparenten Pixeln müssen nach der
// AVIF-Kodierung weiterhin alpha=0 haben.
func TestAVIFKeepsFullyTransparentPixels(t *testing.T) {
	for _, tool := range []string{"magick", "avifenc"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "src.png")
	output := filepath.Join(dir, "out.avif")

	// Roter Kreis mit weichem Rand, rotes RGB auch unter alpha=0.
	mk := exec.Command("magick", "-size", "128x128", "xc:red",
		"(", "-size", "128x128", "xc:black", "-fill", "white", "-draw", "circle 64,64 64,20", "-blur", "0x4", ")",
		"-alpha", "off", "-compose", "CopyOpacity", "-composite", input)
	if out, err := mk.CombinedOutput(); err != nil {
		t.Fatalf("creating test image: %v: %s", err, out)
	}

	_, err := OptimizeWithoutResize(Config{
		InputPath:  input,
		OutputPath: output,
		Quality:    60,
		Silent:     true,
	})
	if err != nil {
		t.Fatalf("OptimizeWithoutResize: %v", err)
	}

	probe := exec.Command("magick", output, "-alpha", "extract", "-format", "%[fx:p{1,1}]", "info:")
	out, err := probe.Output()
	if err != nil {
		t.Fatalf("probing alpha: %v", err)
	}
	if alpha := strings.TrimSpace(string(out)); alpha != "0" {
		t.Fatalf("corner alpha = %s, want 0", alpha)
	}
}
