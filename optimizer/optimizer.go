// pkg/optimizer/optimizer.go
package optimizer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type Config struct {
	InputPath     string
	OutputPath    string
	TargetBytes   int64
	Quality       int
	StripMetadata bool
	Silent        bool
	Verbose       bool
	SkipPostOptim bool
	Width         int
	Height        int
}

type Result struct {
	OriginalSize     int64
	FinalSize        int64
	FinalScale       int
	Iterations       int
	MetadataStripped bool
	OptimizersUsed   []string
	Dimensions       string
}

type Optimizer struct {
	config    Config
	toolCache map[string]*toolInfo
	cacheMux  sync.Mutex
}

type toolInfo struct {
	available bool
	path      string
	checked   bool
}

func New(config Config) *Optimizer {
	return &Optimizer{
		config:    config,
		toolCache: make(map[string]*toolInfo),
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

// OptimizeWithoutResize optimizes image without resizing (format conversion, post-optimization, metadata stripping)
func OptimizeWithoutResize(config Config) (*Result, error) {
	result := &Result{
		FinalScale:       100,
		MetadataStripped: false,
		OptimizersUsed:   []string{},
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

	// Strip metadata
	if config.StripMetadata {
		if config.Verbose {
			fmt.Printf("[verbose] Stripping metadata...\n")
		}
		if err := opt.stripMetadata(config.OutputPath); err != nil {
			if !config.Silent && !config.Verbose {
				fmt.Printf("Warning: metadata stripping failed: %v\n", err)
			}
			if config.Verbose {
				fmt.Printf("[verbose] Metadata stripping failed: %v\n", err)
			}
		} else {
			result.MetadataStripped = true
			result.OptimizersUsed = append(result.OptimizersUsed, "mat2")
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
		OriginalSize:     originalInfo.Size(),
		FinalScale:       100,
		MetadataStripped: false,
		OptimizersUsed:   []string{},
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

		// Strip metadata BEFORE size check (if requested)
		metadataStripped := false
		if o.config.StripMetadata {
			if err := o.stripMetadata(tempOutput); err != nil {
				if !o.config.Silent && !o.config.Verbose {
					fmt.Printf("Warning: metadata stripping failed: %v\n", err)
				}
			} else {
				metadataStripped = true
				iterationOptimizers = append(iterationOptimizers, "mat2")
			}
		}

		// Check file size AFTER post-optimization AND metadata stripping
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
			result.MetadataStripped = metadataStripped
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
	outputExt := strings.ToLower(filepath.Ext(output))

	args := []string{input}

	// Add resize if specified
	if resizeStr != "" {
		args = append(args, "-resize", resizeStr)
		if o.config.Verbose {
			fmt.Printf("[verbose] Resizing to: %s\n", resizeStr)
		}
	}

	args = append(args,
		"-quality", strconv.Itoa(o.config.Quality),
		"-strip", // Strip basic metadata
	)

	// PNG-specific optimizations
	if outputExt == ".png" {
		args = append(args, "-define", "png:compression-level=9")
	}

	args = append(args, output)

	// Check for ImageMagick
	available, _ := o.checkTool("magick")
	cmdName := "magick"
	if !available {
		available, _ = o.checkTool("convert")
		if !available {
			return fmt.Errorf("neither 'magick' nor 'convert' found in PATH")
		}
		cmdName = "convert"
	}

	if o.config.Verbose {
		fmt.Printf("[verbose] Executing: %s %s\n", cmdName, strings.Join(args, " "))
	}

	cmd := exec.Command(cmdName, args...)
	output_bytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("imagemagick failed: %w, output: %s", err, string(output_bytes))
	}

	return nil
}

func (o *Optimizer) resizeImage(input, output string, scalePercent int) error {
	outputExt := strings.ToLower(filepath.Ext(output))

	// Use ImageMagick convert
	args := []string{
		input,
		"-resize", fmt.Sprintf("%d%%", scalePercent),
		"-quality", strconv.Itoa(o.config.Quality),
		"-strip", // Strip basic metadata
	}

	// PNG-specific optimizations
	if outputExt == ".png" {
		args = append(args, "-define", "png:compression-level=9")
	}

	args = append(args, output)

	// Check for ImageMagick
	available, _ := o.checkTool("magick")
	cmdName := "magick"
	if !available {
		available, _ = o.checkTool("convert")
		if !available {
			return fmt.Errorf("neither 'magick' nor 'convert' found in PATH")
		}
		cmdName = "convert"
	}

	if o.config.Verbose {
		fmt.Printf("[verbose] Executing: %s %s\n", cmdName, strings.Join(args, " "))
	}

	cmd := exec.Command(cmdName, args...)
	output_bytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("imagemagick failed: %w, output: %s", err, string(output_bytes))
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
	}

	return optimizers, nil
}

func (o *Optimizer) optimizePNG(filepath string) ([]string, error) {
	optimizers := []string{}

	// Try pngquant
	if available, _ := o.checkTool("pngquant"); available {
		if o.config.Verbose {
			fmt.Printf("[verbose] Executing: pngquant --force --quality=65-95 --output %s %s\n", filepath, filepath)
		}
		cmd := exec.Command("pngquant", "--force", "--quality=65-95", "--output", filepath, filepath)
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

	return optimizers, nil
}

func (o *Optimizer) optimizeJPEG(filepath string) ([]string, error) {
	optimizers := []string{}

	// Try jpegoptim
	if available, _ := o.checkTool("jpegoptim"); available {
		if o.config.Verbose {
			fmt.Printf("[verbose] Executing: jpegoptim --strip-all %s\n", filepath)
		}
		cmd := exec.Command("jpegoptim", "--strip-all", filepath)
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

func (o *Optimizer) stripMetadata(filepath string) error {
	available, _ := o.checkTool("mat2")
	if !available {
		return fmt.Errorf("mat2 not found in PATH")
	}

	if o.config.Verbose {
		fmt.Printf("[verbose] Executing: mat2 --inplace %s\n", filepath)
	}

	cmd := exec.Command("mat2", "--inplace", filepath)
	if err := cmd.Run(); err != nil {
		if o.config.Verbose {
			fmt.Printf("[verbose] mat2 failed: %v\n", err)
		}
		return fmt.Errorf("mat2 failed: %w", err)
	}
	if o.config.Verbose {
		fmt.Printf("[verbose] mat2 completed successfully\n")
	}

	return nil
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
