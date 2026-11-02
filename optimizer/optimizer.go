// pkg/optimizer/optimizer.go
package optimizer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type Config struct {
	InputPath     string
	OutputPath    string
	TargetBytes   int64
	Quality       int
	Silent        bool
	Verbose       bool
	SkipPostOptim bool
	Width         int
	Height        int
}

type Result struct {
	OriginalSize   int64
	FinalSize      int64
	FinalScale     int
	Iterations     int
	OptimizersUsed []string
	Dimensions     string
}

type Optimizer struct {
	config       Config
	toolCache    map[string]*toolInfo
	cacheMux     sync.Mutex
	alphaWarned  bool
	transparency map[string]bool
}

type toolInfo struct {
	available bool
	path      string
	checked   bool
}

func New(config Config) *Optimizer {
	return &Optimizer{
		config:       config,
		toolCache:    make(map[string]*toolInfo),
		transparency: make(map[string]bool),
	}
}

func (o *Optimizer) checkTool(name string) (bool, string) {
	o.cacheMux.Lock()
	defer o.cacheMux.Unlock()

	if info, exists := o.toolCache[name]; exists && info.checked {
		if o.config.Verbose && info.available {
			fmt.Printf("[verbose] Using cached location for %s: %s\n", name, info.path)
		}
		return info.available, info.path
	}

	if o.config.Verbose {
		fmt.Printf("[verbose] Checking for %s...\n", name)
	}

	path, err := exec.LookPath(name)
	info := &toolInfo{
		available: err == nil,
		path:      path,
		checked:   true,
	}
	o.toolCache[name] = info

	if info.available {
		if o.config.Verbose {
			fmt.Printf("[verbose] Found %s at: %s\n", name, path)
		}
	} else {
		if o.config.Verbose {
			fmt.Printf("[verbose] %s not found in PATH\n", name)
		}
	}

	return info.available, path
}

// OptimizeWithoutResize optimizes image without resizing (format conversion, post-optimization)
func OptimizeWithoutResize(config Config) (*Result, error) {
	result := &Result{
		FinalScale:     100,
		OptimizersUsed: []string{},
	}

	// Get original size
	origInfo, err := os.Stat(config.InputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat input: %w", err)
	}
	result.OriginalSize = origInfo.Size()

	if config.Verbose {
		fmt.Printf("[verbose] Input: %s (%s)\n", config.InputPath, formatBytes(origInfo.Size()))
		fmt.Printf("[verbose] Output: %s\n", config.OutputPath)
		fmt.Printf("[verbose] Quality: %d\n", config.Quality)
		if config.Width > 0 || config.Height > 0 {
			fmt.Printf("[verbose] Target dimensions: %dx%d\n", config.Width, config.Height)
		}
	}

	// Convert/copy to output (handles format changes and resizing)
	opt := New(config)

	// Determine resize parameters
	resizeStr := ""
	if config.Width > 0 && config.Height > 0 {
		// Both dimensions specified
		resizeStr = fmt.Sprintf("%dx%d!", config.Width, config.Height)
		result.Dimensions = fmt.Sprintf("%dx%d", config.Width, config.Height)
	} else if config.Width > 0 {
		// Only width specified, maintain aspect ratio
		resizeStr = fmt.Sprintf("%dx", config.Width)
		result.Dimensions = fmt.Sprintf("%dpx width (aspect ratio maintained)", config.Width)
	} else if config.Height > 0 {
		// Only height specified, maintain aspect ratio
		resizeStr = fmt.Sprintf("x%d", config.Height)
		result.Dimensions = fmt.Sprintf("%dpx height (aspect ratio maintained)", config.Height)
	}

	if err := opt.resizeImageWithDimensions(config.InputPath, config.OutputPath, resizeStr); err != nil {
		return nil, fmt.Errorf("failed to process image: %w", err)
	}

	// Post-optimize
	if !config.SkipPostOptim {
		if config.Verbose {
			fmt.Printf("[verbose] Running post-optimization...\n")
		}
		optimizers, err := opt.postOptimize(config.OutputPath)
		if err != nil {
			if !config.Silent && !config.Verbose {
				fmt.Printf("Warning: post-optimization failed: %v\n", err)
			}
			if config.Verbose {
				fmt.Printf("[verbose] Post-optimization failed: %v\n", err)
			}
		} else {
			result.OptimizersUsed = append(result.OptimizersUsed, optimizers...)
		}
	} else {
		if config.Verbose {
			fmt.Printf("[verbose] Post-optimization skipped (--skip-post-optim)\n")
		}
	}

	// Get final size
	finalInfo, err := os.Stat(config.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat output: %w", err)
	}
	result.FinalSize = finalInfo.Size()

	if config.Verbose {
		fmt.Printf("[verbose] Final size: %s\n", formatBytes(result.FinalSize))
	}

	return result, nil
}

func (o *Optimizer) Optimize() (*Result, error) {
	// Get original file size
	originalInfo, err := os.Stat(o.config.InputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat input file: %w", err)
	}

	result := &Result{
		OriginalSize:   originalInfo.Size(),
		FinalScale:     100,
		OptimizersUsed: []string{},
	}

	if o.config.Verbose {
		fmt.Printf("[verbose] Starting optimization with target size: %s\n", formatBytes(o.config.TargetBytes))
		fmt.Printf("[verbose] Input: %s (%s)\n", o.config.InputPath, formatBytes(originalInfo.Size()))
	}

	// Check if already under target size
	if originalInfo.Size() <= o.config.TargetBytes {
		if !o.config.Silent {
			fmt.Println("File already under target size, copying...")
		}
		if err := o.copyWithConversion(o.config.InputPath, o.config.OutputPath, 100); err != nil {
			return nil, err
		}
		// Get actual final size after copy
		if finalInfo, err := os.Stat(o.config.OutputPath); err == nil {
			result.FinalSize = finalInfo.Size()
		}
		return result, nil
	}

	// Binary search for optimal scale
	minScale := 1
	maxScale := 100
	bestScale := 0
	bestSize := int64(0)
	iteration := 0
	var bestOptimizers []string

	for minScale <= maxScale {
		iteration++
		result.Iterations = iteration

		currentScale := (minScale + maxScale) / 2

		if !o.config.Silent {
			fmt.Printf("Iteration %d: trying scale=%d%% (range: %d%%-%d%%)\n",
				iteration, currentScale, minScale, maxScale)
		}

		// Create temp output
		tempOutput := fmt.Sprintf("%s_temp_%d%s",
			strings.TrimSuffix(o.config.OutputPath, filepath.Ext(o.config.OutputPath)),
			iteration,
			filepath.Ext(o.config.OutputPath))

		// Resize and convert from original
		if err := o.resizeImage(o.config.InputPath, tempOutput, currentScale); err != nil {
			return nil, fmt.Errorf("failed to resize: %w", err)
		}

		// Track optimizers used in this iteration
		iterationOptimizers := []string{}

		// Post-optimization BEFORE size check
		if !o.config.SkipPostOptim {
			optimizers, err := o.postOptimize(tempOutput)
			if err != nil {
				if !o.config.Silent && !o.config.Verbose {
					fmt.Printf("Warning: post-optimization failed: %v\n", err)
				}
			} else {
				iterationOptimizers = append(iterationOptimizers, optimizers...)
			}
		}

		// Check file size AFTER post-optimization
		info, err := os.Stat(tempOutput)
		if err != nil {
			os.Remove(tempOutput) // Clean up on error
			return nil, fmt.Errorf("failed to stat temp file: %w", err)
		}

		if !o.config.Silent {
			fmt.Printf("  → size=%s", formatBytes(info.Size()))
		}

		if info.Size() <= o.config.TargetBytes {
			// File fits! This is a valid solution
			// Try to find a larger scale that still fits (better quality)
			bestScale = currentScale
			bestSize = info.Size()
			bestOptimizers = iterationOptimizers

			if !o.config.Silent {
				fmt.Printf(" ✓ (under target, trying larger)\n")
			}

			// Keep this file temporarily in case it's the best
			bestTempOutput := fmt.Sprintf("%s_best%s",
				strings.TrimSuffix(o.config.OutputPath, filepath.Ext(o.config.OutputPath)),
				filepath.Ext(o.config.OutputPath))
			os.Remove(bestTempOutput) // Remove old best if exists
			os.Rename(tempOutput, bestTempOutput)

			// Search for larger scale
			minScale = currentScale + 1

		} else {
			// File too large, try smaller scale
			if !o.config.Silent {
				fmt.Printf(" ✗ (too large, trying smaller)\n")
			}
			os.Remove(tempOutput) // Remove immediately to save disk space
			maxScale = currentScale - 1
		}
	}

	// Check if we found a valid solution
	if bestScale == 0 {
		return nil, fmt.Errorf("could not reach target size even at 1%% scale")
	}

	// Move best result to final output
	bestTempOutput := fmt.Sprintf("%s_best%s",
		strings.TrimSuffix(o.config.OutputPath, filepath.Ext(o.config.OutputPath)),
		filepath.Ext(o.config.OutputPath))

	if err := os.Rename(bestTempOutput, o.config.OutputPath); err != nil {
		os.Remove(bestTempOutput)
		return nil, fmt.Errorf("failed to move final file: %w", err)
	}

	result.FinalScale = bestScale
	result.FinalSize = bestSize
	result.Iterations = iteration
	result.OptimizersUsed = bestOptimizers

	// Cleanup any remaining temp files
	o.cleanupTempFiles()

	if o.config.Verbose {
		fmt.Printf("[verbose] Optimization complete: %d%% scale, %s final size\n", bestScale, formatBytes(bestSize))
	}

	return result, nil
}

func (o *Optimizer) resizeImageWithDimensions(input, output string, resizeStr string) error {
	args := []string{input}

	// Add resize if specified
	if resizeStr != "" {
		args = append(args, "-resize", resizeStr)
		if o.config.Verbose {
			fmt.Printf("[verbose] Resizing to: %s\n", resizeStr)
		}
	}

	return o.encode(args, output)
}

func (o *Optimizer) resizeImage(input, output string, scalePercent int) error {
	args := []string{
		input,
		"-resize", fmt.Sprintf("%d%%", scalePercent),
	}

	return o.encode(args, output)
}

// encode writes the prepared ImageMagick pipeline (input + resize args) to output.
// Transparent images go to AVIF through avifenc when available, because
// ImageMagick encodes the alpha channel lossy, which lifts alpha=0 pixels and
// causes a grey haze. Opaque images stay on the smaller ImageMagick output.
// All metadata is stripped except an RGB color profile, which is kept.
func (o *Optimizer) encode(magickArgs []string, output string) error {
	outputExt := strings.ToLower(filepath.Ext(output))

	profile := o.extractRGBProfile(magickArgs[0], output)
	if profile != "" {
		defer os.Remove(profile)
	}

	if outputExt == ".avif" && o.hasTransparency(magickArgs[0]) {
		if available, _ := o.checkTool("avifenc"); available {
			return o.encodeAVIF(magickArgs, profile, output)
		}
		o.warnLossyAlpha()
	}

	if outputExt == ".webp" {
		return o.encodeWebP(magickArgs, profile, output)
	}

	isJPEG := outputExt == ".jpg" || outputExt == ".jpeg"
	if isJPEG {
		if cjpeg := o.mozjpegPath(); cjpeg != "" {
			return o.encodeMozJPEG(cjpeg, magickArgs, profile, output)
		}
	}

	args := slices.Clone(magickArgs)
	args = append(args, "-quality", strconv.Itoa(o.config.Quality))
	args = append(args, stripArgs(profile)...)

	// ImageMagick switches to full chroma resolution from quality 90 on, which
	// makes files much larger; progressive saves about 10 % losslessly.
	if isJPEG {
		args = append(args, "-sampling-factor", "4:2:0", "-interlace", "Plane")
	}

	// Slowest AV1 encoder speed for the smallest output (about −9 %).
	if outputExt == ".avif" {
		args = append(args, "-define", "heic:speed=0")
	}

	// PNG-specific optimizations
	if outputExt == ".png" {
		args = append(args, "-define", "png:compression-level=9")
	}

	args = append(args, output)

	if err := o.runImageMagick(args); err != nil {
		return err
	}

	if outputExt == ".png" && profile != "" {
		return o.attachPNGProfile(profile, output)
	}

	return nil
}

// extractRGBProfile writes the ICC profile of input next to output and returns
// its path, or "" if input has no profile. Only RGB profiles are kept: the
// output is always RGB, so a CMYK or grey profile would render wrong colors.
func (o *Optimizer) extractRGBProfile(input, output string) string {
	profile := strings.TrimSuffix(output, filepath.Ext(output)) + "_profile.icc"
	if err := o.runImageMagick([]string{input, "icc:" + profile}); err != nil {
		return ""
	}

	// Bytes 16–19 of an ICC header hold the color space signature.
	header, err := os.ReadFile(profile)
	if err != nil || len(header) < 20 || string(header[16:20]) != "RGB " {
		os.Remove(profile)
		return ""
	}

	return profile
}

// stripArgs removes all metadata and re-attaches the color profile, if any.
// Assigning a profile to an image without one embeds it as is, pixels are not
// converted.
func stripArgs(profile string) []string {
	if profile == "" {
		return []string{"-strip"}
	}
	return []string{"-strip", "-profile", profile}
}

// attachPNGProfile embeds profile into the already stripped PNG. ImageMagick's
// PNG writer drops a profile attached after -strip, so it needs a second pass.
func (o *Optimizer) attachPNGProfile(profile, output string) error {
	return o.runImageMagick([]string{
		output,
		"-profile", profile,
		"-define", "png:compression-level=9",
		"-define", "png:exclude-chunk=date,tIME",
		output,
	})
}

// webpArgs builds the ImageMagick arguments for a WebP encode. method=6 is the
// slowest, most thorough libwebp method for the smallest output. Sharp YUV is
// left out on purpose: it adds 4–13 % size. In lossless mode quality sets the
// compression effort, not the fidelity.
func webpArgs(magickArgs []string, profile string, quality int, lossless bool, output string) []string {
	args := slices.Clone(magickArgs)
	args = append(args, "-quality", strconv.Itoa(quality))
	args = append(args, stripArgs(profile)...)
	args = append(args, "-define", "webp:method=6")
	if lossless {
		args = append(args, "-define", "webp:lossless=true")
	}

	return append(args, output)
}

// mozjpegCandidates lists where cjpeg from mozjpeg is usually found. Homebrew
// installs mozjpeg keg-only (not on PATH) because it conflicts with
// libjpeg-turbo; a source build installs to /opt/mozjpeg.
var mozjpegCandidates = []string{
	"cjpeg",
	"/opt/homebrew/opt/mozjpeg/bin/cjpeg",
	"/usr/local/opt/mozjpeg/bin/cjpeg",
	"/opt/mozjpeg/bin/cjpeg",
}

// mozjpegPath returns the path of a cjpeg built from mozjpeg, or "" if none is
// installed. A cjpeg from libjpeg-turbo is ignored.
func (o *Optimizer) mozjpegPath() string {
	o.cacheMux.Lock()
	defer o.cacheMux.Unlock()

	if info, ok := o.toolCache["mozjpeg"]; ok {
		return info.path
	}

	info := &toolInfo{checked: true}
	for _, candidate := range mozjpegCandidates {
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		version, _ := exec.Command(path, "-version").CombinedOutput()
		if strings.Contains(string(version), "mozjpeg") {
			info.available = true
			info.path = path
			break
		}
	}
	o.toolCache["mozjpeg"] = info

	if o.config.Verbose {
		if info.available {
			fmt.Printf("[verbose] Found mozjpeg at: %s\n", info.path)
		} else {
			fmt.Printf("[verbose] mozjpeg not found, encoding JPEG with ImageMagick\n")
		}
	}

	return info.path
}

// encodeMozJPEG renders the ImageMagick pipeline to a temp PPM and encodes it
// with mozjpeg, about 20 % smaller than ImageMagick at the same quality.
// mozjpeg writes progressive JPEGs by default; 4:2:0 is forced because mozjpeg
// otherwise picks 4:2:2 or 4:4:4 depending on quality.
func (o *Optimizer) encodeMozJPEG(cjpeg string, magickArgs []string, profile, output string) error {
	tempPPM := strings.TrimSuffix(output, filepath.Ext(output)) + "_mozsrc.ppm"
	defer os.Remove(tempPPM)

	if err := o.runImageMagick(append(slices.Clone(magickArgs), "-strip", "-depth", "8", tempPPM)); err != nil {
		return err
	}

	args := []string{
		"-quality", strconv.Itoa(o.config.Quality),
		"-sample", "2x2",
		"-outfile", output,
	}
	if profile != "" {
		args = append(args, "-icc", profile)
	}
	args = append(args, tempPPM)

	if o.config.Verbose {
		fmt.Printf("[verbose] Executing: %s %s\n", cjpeg, strings.Join(args, " "))
	}

	if outputBytes, err := exec.Command(cjpeg, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("mozjpeg failed: %w, output: %s", err, string(outputBytes))
	}

	return nil
}

// encodeWebP encodes both lossy and lossless and keeps the smaller file.
// Flat graphics, screenshots and logos are often several times smaller
// lossless, while photos are much smaller lossy.
func (o *Optimizer) encodeWebP(magickArgs []string, profile, output string) error {
	if err := o.runImageMagick(webpArgs(magickArgs, profile, o.config.Quality, false, output)); err != nil {
		return err
	}

	losslessOutput := strings.TrimSuffix(output, filepath.Ext(output)) + "_lossless.webp"
	defer os.Remove(losslessOutput)

	if err := o.runImageMagick(webpArgs(magickArgs, profile, 100, true, losslessOutput)); err != nil {
		return err
	}

	lossyInfo, err := os.Stat(output)
	if err != nil {
		return err
	}
	losslessInfo, err := os.Stat(losslessOutput)
	if err != nil {
		return err
	}

	if losslessInfo.Size() >= lossyInfo.Size() {
		return nil
	}

	if o.config.Verbose {
		fmt.Printf("[verbose] WebP lossless is smaller (%s vs. %s lossy), keeping lossless\n",
			formatBytes(losslessInfo.Size()), formatBytes(lossyInfo.Size()))
	}

	return os.Rename(losslessOutput, output)
}

// avifencArgs builds the avifenc argument list for encoding tempPNG to output
// at the given quality. Premultiplied alpha is signalled explicitly: some
// (mostly weaker/older Android) AVIF decoders blend non-premultiplied alpha
// incorrectly and let color bleed through fully or partially transparent
// pixels as a visible grey/dark haze, even though the bitstream itself is
// correct. Speed 0 and 4:2:0 give the smallest files (speed 0: −7 to −12 %,
// 4:2:0 instead of avifenc's 4:4:4 default for PNG input: another −5 %).
func avifencArgs(quality int, tempPNG, output string) []string {
	return []string{
		"-q", strconv.Itoa(quality),
		"-s", "0",
		"-y", "420",
		"--qalpha", "100",
		"--premultiply",
		tempPNG,
		output,
	}
}

// encodeAVIF renders the ImageMagick pipeline to a lossless temp PNG and encodes
// it with avifenc using lossless, premultiplied alpha.
func (o *Optimizer) encodeAVIF(magickArgs []string, profile, output string) error {
	tempPNG := strings.TrimSuffix(output, filepath.Ext(output)) + "_avifsrc.png"
	defer os.Remove(tempPNG)

	if err := o.runImageMagick(append(slices.Clone(magickArgs), "-strip", tempPNG)); err != nil {
		return err
	}

	args := avifencArgs(o.config.Quality, tempPNG, output)
	if profile != "" {
		args = append([]string{"--icc", profile}, args...)
	}

	if o.config.Verbose {
		fmt.Printf("[verbose] Executing: avifenc %s\n", strings.Join(args, " "))
	}

	cmd := exec.Command("avifenc", args...)
	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("avifenc failed: %w, output: %s", err, string(outputBytes))
	}

	return nil
}

// warnLossyAlpha prints a one-time warning when an image with transparency is
// encoded to AVIF by ImageMagick because avifenc is missing.
func (o *Optimizer) warnLossyAlpha() {
	if o.alphaWarned || o.config.Silent {
		return
	}
	o.alphaWarned = true
	fmt.Printf("Warning: avifenc not found, ImageMagick encodes the alpha channel lossy (grey haze on dark backgrounds possible). Install libavif (avifenc) for lossless alpha.\n")
}

func (o *Optimizer) hasTransparency(input string) bool {
	if cached, ok := o.transparency[input]; ok {
		return cached
	}
	result := o.probeTransparency(input)
	o.transparency[input] = result
	return result
}

func (o *Optimizer) probeTransparency(input string) bool {
	cmdName, err := o.imageMagickCommand()
	if err != nil {
		return false
	}

	var cmd *exec.Cmd
	if cmdName == "magick" {
		cmd = exec.Command("magick", "identify", "-format", "%[opaque]", input)
	} else {
		cmd = exec.Command("identify", "-format", "%[opaque]", input)
	}

	outputBytes, err := cmd.Output()
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(outputBytes)) == "False"
}

func (o *Optimizer) imageMagickCommand() (string, error) {
	if available, _ := o.checkTool("magick"); available {
		return "magick", nil
	}
	if available, _ := o.checkTool("convert"); available {
		return "convert", nil
	}
	return "", fmt.Errorf("neither 'magick' nor 'convert' found in PATH")
}

func (o *Optimizer) runImageMagick(args []string) error {
	cmdName, err := o.imageMagickCommand()
	if err != nil {
		return err
	}

	if o.config.Verbose {
		fmt.Printf("[verbose] Executing: %s %s\n", cmdName, strings.Join(args, " "))
	}

	cmd := exec.Command(cmdName, args...)
	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("imagemagick failed: %w, output: %s", err, string(outputBytes))
	}

	return nil
}

func (o *Optimizer) postOptimize(filepath string) ([]string, error) {
	ext := strings.ToLower(filepath[len(filepath)-4:])
	optimizers := []string{}

	if o.config.Verbose {
		fmt.Printf("[verbose] Post-optimizing %s file...\n", ext)
	}

	switch ext {
	case ".png":
		used, err := o.optimizePNG(filepath)
		if err != nil {
			return optimizers, err
		}
		optimizers = append(optimizers, used...)
	case ".jpg", "jpeg":
		used, err := o.optimizeJPEG(filepath)
		if err != nil {
			return optimizers, err
		}
		optimizers = append(optimizers, used...)
	case ".gif":
		used, err := o.optimizeGIF(filepath)
		if err != nil {
			return optimizers, err
		}
		optimizers = append(optimizers, used...)
	}

	return optimizers, nil
}

func (o *Optimizer) optimizePNG(filepath string) ([]string, error) {
	optimizers := []string{}

	// Try pngquant. It converts images with a color profile to sRGB and drops
	// the profile; accepted on purpose, because skipping it makes such PNGs
	// 2–3.6 times larger.
	if available, _ := o.checkTool("pngquant"); available {
		if o.config.Verbose {
			fmt.Printf("[verbose] Executing: pngquant --force --quality=0-95 --output %s %s\n", filepath, filepath)
		}
		cmd := exec.Command("pngquant", "--force", "--quality=0-95", "--output", filepath, filepath)
		if err := cmd.Run(); err != nil {
			if !o.config.Silent && !o.config.Verbose {
				fmt.Printf("pngquant failed: %v\n", err)
			}
			if o.config.Verbose {
				fmt.Printf("[verbose] pngquant failed: %v\n", err)
			}
		} else {
			optimizers = append(optimizers, "pngquant")
			if o.config.Verbose {
				fmt.Printf("[verbose] pngquant completed successfully\n")
			}
		}
	}

	// Try oxipng as secondary optimization
	if available, _ := o.checkTool("oxipng"); available {
		if o.config.Verbose {
			fmt.Printf("[verbose] Executing: oxipng -o 3 -i 0 --strip safe %s\n", filepath)
		}
		cmd := exec.Command("oxipng", "-o", "3", "-i", "0", "--strip", "safe", filepath)
		if err := cmd.Run(); err != nil {
			if !o.config.Silent && !o.config.Verbose {
				fmt.Printf("oxipng failed: %v\n", err)
			}
			if o.config.Verbose {
				fmt.Printf("[verbose] oxipng failed: %v\n", err)
			}
		} else {
			optimizers = append(optimizers, "oxipng")
			if o.config.Verbose {
				fmt.Printf("[verbose] oxipng completed successfully\n")
			}
		}
	}

	// Try zopflipng as final lossless recompression
	if available, _ := o.checkTool("zopflipng"); available {
		if o.config.Verbose {
			fmt.Printf("[verbose] Executing: zopflipng -y -m --keepchunks=iCCP,sRGB %s %s\n", filepath, filepath)
		}
		cmd := exec.Command("zopflipng", "-y", "-m", "--keepchunks=iCCP,sRGB", filepath, filepath)
		if err := cmd.Run(); err != nil {
			if !o.config.Silent && !o.config.Verbose {
				fmt.Printf("zopflipng failed: %v\n", err)
			}
			if o.config.Verbose {
				fmt.Printf("[verbose] zopflipng failed: %v\n", err)
			}
		} else {
			optimizers = append(optimizers, "zopflipng")
			if o.config.Verbose {
				fmt.Printf("[verbose] zopflipng completed successfully\n")
			}
		}
	}

	return optimizers, nil
}

func (o *Optimizer) optimizeJPEG(filepath string) ([]string, error) {
	optimizers := []string{}

	// Try jpegoptim
	if available, _ := o.checkTool("jpegoptim"); available {
		if o.config.Verbose {
			fmt.Printf("[verbose] Executing: jpegoptim --strip-all --keep-icc %s\n", filepath)
		}
		cmd := exec.Command("jpegoptim", "--strip-all", "--keep-icc", filepath)
		if err := cmd.Run(); err != nil {
			if o.config.Verbose {
				fmt.Printf("[verbose] jpegoptim failed: %v\n", err)
			}
			return optimizers, fmt.Errorf("jpegoptim failed: %w", err)
		}
		optimizers = append(optimizers, "jpegoptim")
		if o.config.Verbose {
			fmt.Printf("[verbose] jpegoptim completed successfully\n")
		}
	}

	return optimizers, nil
}

// optimizeGIF recompresses losslessly with gifsicle. Animations shrink most,
// because -O3 stores only the changed area of each frame.
func (o *Optimizer) optimizeGIF(filepath string) ([]string, error) {
	optimizers := []string{}

	if available, _ := o.checkTool("gifsicle"); available {
		if o.config.Verbose {
			fmt.Printf("[verbose] Executing: gifsicle -b -O3 %s\n", filepath)
		}
		cmd := exec.Command("gifsicle", "-b", "-O3", filepath)
		if outputBytes, err := cmd.CombinedOutput(); err != nil {
			if o.config.Verbose {
				fmt.Printf("[verbose] gifsicle failed: %v\n", err)
			}
			return optimizers, fmt.Errorf("gifsicle failed: %w, output: %s", err, string(outputBytes))
		}
		optimizers = append(optimizers, "gifsicle")
		if o.config.Verbose {
			fmt.Printf("[verbose] gifsicle completed successfully\n")
		}
	}

	return optimizers, nil
}

func (o *Optimizer) copyWithConversion(input, output string, scale int) error {
	return o.resizeImage(input, output, scale)
}

func (o *Optimizer) cleanupTempFiles() {
	// Clean up temp iteration files
	pattern := fmt.Sprintf("%s_temp_*%s",
		strings.TrimSuffix(o.config.OutputPath, filepath.Ext(o.config.OutputPath)),
		filepath.Ext(o.config.OutputPath))

	matches, _ := filepath.Glob(pattern)
	for _, match := range matches {
		if err := os.Remove(match); err != nil && !o.config.Silent {
			fmt.Printf("Warning: failed to remove temp file %s: %v\n", match, err)
		}
	}

	// Clean up best file if exists
	bestFile := fmt.Sprintf("%s_best%s",
		strings.TrimSuffix(o.config.OutputPath, filepath.Ext(o.config.OutputPath)),
		filepath.Ext(o.config.OutputPath))
	os.Remove(bestFile) // Ignore error, might not exist
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
