package optimizer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// wideGamutTestProfiles lists RGB profiles other than sRGB shipped with common
// systems; the test is skipped if none of them exists.
var wideGamutTestProfiles = []string{
	"/System/Library/ColorSync/Profiles/Display P3.icc",
	"/usr/share/color/icc/colord/DisplayP3.icc",
	"/usr/share/color/icc/colord/AdobeRGB1998.icc",
}

// Bilder mit Farbprofil werden auf jedem Kodierweg nach sRGB umgerechnet und
// ohne Profil gespeichert. Browser behandeln Bilder ohne Profil als sRGB, die
// Farben bleiben also gleich.
func TestProfiledImageIsConvertedToSRGB(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("magick not installed")
	}
	profile := ""
	for _, candidate := range wideGamutTestProfiles {
		if _, err := os.Stat(candidate); err == nil {
			profile = candidate
			break
		}
	}
	if profile == "" {
		t.Skip("no wide-gamut ICC profile found on this system")
	}

	dir := t.TempDir()
	opaque := filepath.Join(dir, "opaque.jpg")
	transparent := filepath.Join(dir, "transparent.png")
	createImage(t, "-size", "64x64", "xc:rgb(200,80,40)", "-profile", profile, "-quality", "100", opaque)
	createImage(t, "-size", "64x64", "xc:rgb(200,80,40)", "-alpha", "set", "-channel", "A", "-evaluate", "set", "50%", "+channel",
		"-profile", profile, transparent)

	srgb := filepath.Join(dir, "srgb.icc")
	if err := os.WriteFile(srgb, srgbProfile, 0o644); err != nil {
		t.Fatal(err)
	}
	want := centerPixel(t, opaque, "-profile", srgb)
	if raw := centerPixel(t, opaque); colorDistance(raw, want) < 10 {
		t.Fatalf("test color %v barely changes in sRGB (%v), test would prove nothing", raw, want)
	}

	cases := []struct{ name, input, ext string }{
		{"jpg", opaque, "jpg"},
		{"png", opaque, "png"},
		{"webp", opaque, "webp"},
		{"avif", opaque, "avif"},
		{"png with alpha", transparent, "png"},
		{"webp with alpha", transparent, "webp"},
		{"avif with alpha", transparent, "avif"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertConvertedToSRGB(t, c.input, filepath.Join(t.TempDir(), "out."+c.ext), want)
		})
	}

	t.Run("jpg without mozjpeg", func(t *testing.T) {
		saved := mozjpegCandidates
		mozjpegCandidates = nil
		t.Cleanup(func() { mozjpegCandidates = saved })

		assertConvertedToSRGB(t, opaque, filepath.Join(t.TempDir(), "out.jpg"), want)
	})
}

func assertConvertedToSRGB(t *testing.T, input, output string, want [3]int) {
	t.Helper()

	if _, err := OptimizeWithoutResize(Config{InputPath: input, OutputPath: output, Quality: 80, Silent: true}); err != nil {
		t.Fatalf("OptimizeWithoutResize: %v", err)
	}

	if err := exec.Command("magick", output, "icc:-").Run(); err == nil {
		t.Error("output still has a color profile")
	}

	// ImageMagick ignores the premultiplied-alpha flag when reading AVIF and
	// returns darkened colors; avifdec decodes it correctly.
	decoded := output
	if filepath.Ext(output) == ".avif" {
		if _, err := exec.LookPath("avifdec"); err == nil {
			decoded = output + ".png"
			if out, err := exec.Command("avifdec", output, decoded).CombinedOutput(); err != nil {
				t.Fatalf("avifdec: %v: %s", err, out)
			}
		}
	}
	if got := centerPixel(t, decoded); colorDistance(got, want) > 4 {
		t.Errorf("center pixel = %v, want about %v (sRGB)", got, want)
	}
}

// centerPixel returns the RGB values (0–255) of the center pixel after
// applying the optional ImageMagick operations.
func centerPixel(t *testing.T, path string, operations ...string) [3]int {
	t.Helper()
	args := append([]string{path}, operations...)
	args = append(args, "-alpha", "off", "-format",
		"%[fx:round(255*p{32,32}.r)] %[fx:round(255*p{32,32}.g)] %[fx:round(255*p{32,32}.b)]", "info:")
	out, err := exec.Command("magick", args...).Output()
	if err != nil {
		t.Fatalf("reading pixel of %s: %v", path, err)
	}

	var rgb [3]int
	if _, err := fmt.Sscan(strings.TrimSpace(string(out)), &rgb[0], &rgb[1], &rgb[2]); err != nil {
		t.Fatalf("parsing pixel %q: %v", out, err)
	}
	return rgb
}

// colorDistance returns the largest difference of a single channel.
func colorDistance(a, b [3]int) int {
	maxDiff := 0
	for i := range a {
		maxDiff = max(maxDiff, abs(a[i]-b[i]))
	}
	return maxDiff
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func createImage(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("magick", args...).CombinedOutput(); err != nil {
		t.Fatalf("creating test image: %v: %s", err, out)
	}
}
