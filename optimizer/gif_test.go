package optimizer

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// Animierte GIFs werden mit gifsicle verlustfrei nachoptimiert: kleiner, gleiche
// Bildanzahl, gleiche Pixel.
func TestGIFIsOptimizedLosslessWithGifsicle(t *testing.T) {
	for _, tool := range []string{"magick", "gifsicle"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "src.gif")
	mk := exec.Command("magick", "-delay", "10", "-size", "200x200", "xc:white",
		"(", "+clone", "-fill", "red", "-draw", "circle 100,100 100,50", ")",
		"(", "+clone", "-fill", "blue", "-draw", "rectangle 20,20 80,80", ")", input)
	if out, err := mk.CombinedOutput(); err != nil {
		t.Fatalf("creating test image: %v: %s", err, out)
	}

	plain := filepath.Join(dir, "plain.gif")
	optimized := filepath.Join(dir, "optimized.gif")
	if _, err := OptimizeWithoutResize(Config{InputPath: input, OutputPath: plain, Silent: true, SkipPostOptim: true}); err != nil {
		t.Fatalf("encode without post-optimization: %v", err)
	}
	result, err := OptimizeWithoutResize(Config{InputPath: input, OutputPath: optimized, Silent: true})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if !slices.Contains(result.OptimizersUsed, "gifsicle") {
		t.Fatalf("optimizers = %v, want gifsicle", result.OptimizersUsed)
	}

	plainInfo, _ := os.Stat(plain)
	optimizedInfo, _ := os.Stat(optimized)
	if optimizedInfo.Size() >= plainInfo.Size() {
		t.Errorf("size %d, want smaller than %d", optimizedInfo.Size(), plainInfo.Size())
	}

	// Verglichen werden die vollständig zusammengesetzten Einzelbilder, weil
	// gifsicle nur die geänderten Bereiche pro Bild speichert.
	plainFrames := coalesceFrames(t, plain)
	optimizedFrames := coalesceFrames(t, optimized)
	cmp := exec.Command("magick", "compare", "-metric", "AE", plainFrames, optimizedFrames, "null:")
	if out, err := cmp.CombinedOutput(); err != nil {
		t.Errorf("frames differ after gifsicle: %s", out)
	}
}

// coalesceFrames setzt alle Einzelbilder eines GIFs untereinander in ein PNG.
func coalesceFrames(t *testing.T, gif string) string {
	t.Helper()
	png := gif + ".png"
	if out, err := exec.Command("magick", gif, "-coalesce", "-append", png).CombinedOutput(); err != nil {
		t.Fatalf("coalescing %s: %v: %s", gif, err, out)
	}
	return png
}
