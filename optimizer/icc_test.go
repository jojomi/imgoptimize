package optimizer

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// iccTestProfiles lists RGB profiles shipped with common systems; the test is
// skipped if none of them exists.
var iccTestProfiles = []string{
	"/System/Library/ColorSync/Profiles/Display P3.icc",
	"/usr/share/color/icc/colord/DisplayP3.icc",
	"/usr/share/color/icc/colord/AdobeRGB1998.icc",
}

// Ein eingebettetes Farbprofil muss in jedem Ausgabeformat und auf jedem
// Kodierweg erhalten bleiben, persönliche Metadaten dagegen nicht.
func TestICCProfileIsKept(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("magick not installed")
	}
	profile := findTestProfile(t)
	want, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	opaque := filepath.Join(dir, "opaque.jpg")
	transparent := filepath.Join(dir, "transparent.png")
	makeImage(t, "-seed", "1", "-size", "64x64", "plasma:", "-profile", profile, "-set", "comment", "Secret", opaque)
	makeImage(t, "-seed", "1", "-size", "64x64", "plasma:", "-alpha", "set", "-channel", "A", "-evaluate", "set", "50%", "+channel",
		"-profile", profile, transparent)

	// PNG ohne Nachoptimierung, weil pngquant bewusst nach sRGB umrechnet
	// (siehe TestPNGQuantizedWithProfileIsTaggedSRGB).
	cases := []struct {
		name, input, ext string
		skipPostOptim    bool
	}{
		{"jpg", opaque, "jpg", false},
		{"png", opaque, "png", true},
		{"webp", opaque, "webp", false},
		{"avif", opaque, "avif", false},
		{"png with alpha", transparent, "png", true},
		{"webp with alpha", transparent, "webp", false},
		{"avif with alpha", transparent, "avif", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "out."+c.ext)
			assertKeepsProfile(t, c.input, output, c.skipPostOptim, want)
		})
	}

	t.Run("jpg without mozjpeg", func(t *testing.T) {
		saved := mozjpegCandidates
		mozjpegCandidates = nil
		t.Cleanup(func() { mozjpegCandidates = saved })

		assertKeepsProfile(t, opaque, filepath.Join(t.TempDir(), "out.jpg"), false, want)
	})
}

// pngquant rechnet PNGs mit Farbprofil nach sRGB um. Das Ergebnis muss dann
// als sRGB markiert sein und darf kein (nun falsches) Profil mehr tragen.
func TestPNGQuantizedWithProfileIsTaggedSRGB(t *testing.T) {
	for _, tool := range []string{"magick", "pngquant"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	profile := findTestProfile(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "src.jpg")
	output := filepath.Join(dir, "out.png")
	makeImage(t, "-seed", "1", "-size", "64x64", "plasma:", "-profile", profile, input)

	if _, err := OptimizeWithoutResize(Config{InputPath: input, OutputPath: output, Quality: 80, Silent: true}); err != nil {
		t.Fatalf("OptimizeWithoutResize: %v", err)
	}

	chunks := pngChunks(t, output)
	if !slices.Contains(chunks, "sRGB") || slices.Contains(chunks, "iCCP") {
		t.Fatalf("chunks = %v, want sRGB and no iCCP", chunks)
	}
}

func findTestProfile(t *testing.T) string {
	t.Helper()
	for _, candidate := range iccTestProfiles {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Skip("no RGB ICC profile found on this system")
	return ""
}

// pngChunks returns the chunk types of a PNG file in order.
func pngChunks(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var chunks []string
	for i := 8; i+8 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[i:]))
		chunks = append(chunks, string(data[i+4:i+8]))
		i += 12 + length
	}
	return chunks
}

func assertKeepsProfile(t *testing.T, input, output string, skipPostOptim bool, want []byte) {
	t.Helper()

	config := Config{InputPath: input, OutputPath: output, Quality: 80, Silent: true, SkipPostOptim: skipPostOptim}
	if _, err := OptimizeWithoutResize(config); err != nil {
		t.Fatalf("OptimizeWithoutResize: %v", err)
	}

	got, err := exec.Command("magick", output, "icc:-").Output()
	if err != nil {
		t.Fatalf("no ICC profile in output: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ICC profile changed: got %d bytes, want %d", len(got), len(want))
	}

	comment, _ := exec.Command("magick", "identify", "-format", "%c", output).Output()
	if bytes.Contains(comment, []byte("Secret")) {
		t.Fatal("comment was not stripped")
	}
}

func makeImage(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("magick", args...).CombinedOutput(); err != nil {
		t.Fatalf("creating test image: %v: %s", err, out)
	}
}
