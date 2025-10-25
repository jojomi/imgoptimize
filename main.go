package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jojomi/imgoptimize/optimizer"
	"github.com/spf13/cobra"
)

var (
	version = "0.1.0"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var (
	outputPath    string
	quality       int
	stripMetadata bool
	silent        bool
	verbose       bool
	inplace       bool
	skipPostOptim bool
	width         int
	height        int
)

var rootCmd = &cobra.Command{
	Use:   "imgoptimize [input-file]",
	Short: "Optimize image files (no scaling)",
	Long: `imgoptimize optimizes image files using pngquant/oxipng/jpegoptim/mat2.
Optionally resize with --width and/or --height (maintains aspect ratio if only one is set).
Use the scale-down-to-filesize subcommand if you want to resize to a target file size.`,
	Args:    cobra.ExactArgs(1),
	RunE:    runJustOptimize,
	Version: version,
}

var scaleDownToFilesizeCmd = &cobra.Command{
	Use:   "scale-down-to-filesize [target-size] [input-file]",
	Short: "Scale down image to target file size",
	Long:  `Scale down an image file to reach a target file size using binary search for optimal quality.`,
	Args:  cobra.ExactArgs(2),
	RunE:  runOptimizeWithTarget,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&outputPath, "output", "o", "", "output file path, format (jpg/png/webp), or :filename for input-relative path")
	rootCmd.PersistentFlags().IntVarP(&quality, "quality", "q", 85, "image quality (0-100)")
	rootCmd.PersistentFlags().BoolVar(&stripMetadata, "strip-metadata", false, "strip metadata using mat2")
	rootCmd.PersistentFlags().BoolVarP(&silent, "silent", "s", false, "no output")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output (show optimizer discovery and execution)")
	rootCmd.PersistentFlags().BoolVarP(&inplace, "inplace", "i", false, "optimize file in-place")
	rootCmd.PersistentFlags().BoolVar(&skipPostOptim, "skip-post-optim", false, "skip post-optimization with pngquant/jpegoptim")

	// Root command specific flags for dimensions
	rootCmd.Flags().IntVar(&width, "width", 0, "target width in pixels (maintains aspect ratio if height not set)")
	rootCmd.Flags().IntVar(&height, "height", 0, "target height in pixels (maintains aspect ratio if width not set)")

	rootCmd.AddCommand(scaleDownToFilesizeCmd)
}

// Root command: optimize with optional resizing
func runJustOptimize(cmd *cobra.Command, args []string) error {
	inputFile := args[0]

	if _, err := os.Stat(inputFile); os.IsNotExist(err) {
		return fmt.Errorf("input file does not exist: %s", inputFile)
	}

	output, err := resolveOutputPath(inputFile, outputPath, inplace)
	if err != nil {
		return err
	}

	origStat, _ := os.Stat(inputFile)

	// Use shared optimization function
	result, err := optimizer.OptimizeWithoutResize(optimizer.Config{
		InputPath:     inputFile,
		OutputPath:    output,
		Quality:       quality,
		StripMetadata: stripMetadata,
		Silent:        silent,
		Verbose:       verbose,
		SkipPostOptim: skipPostOptim,
		Width:         width,
		Height:        height,
	})
	if err != nil {
		return fmt.Errorf("optimization failed: %w", err)
	}

	if !silent {
		fmt.Printf("✓ Optimized: %s → %s\n", inputFile, output)
		fmt.Printf("  Original size: %s\n", formatBytes(origStat.Size()))
		fmt.Printf("  Final size: %s\n", formatBytes(result.FinalSize))
		if result.Dimensions != "" {
			fmt.Printf("  Dimensions: %s\n", result.Dimensions)
		}
		if len(result.OptimizersUsed) > 0 {
			fmt.Printf("  Optimizers: %s\n", strings.Join(result.OptimizersUsed, ", "))
		}
		if result.MetadataStripped {
			fmt.Printf("  Metadata: stripped\n")
		}
	}

	return nil
}

// Subcommand: optimize with resizing to target size
func runOptimizeWithTarget(cmd *cobra.Command, args []string) error {
	targetSizeStr := args[0]
	inputFile := args[1]

	if _, err := os.Stat(inputFile); os.IsNotExist(err) {
		return fmt.Errorf("input file does not exist: %s", inputFile)
	}

	output, err := resolveOutputPath(inputFile, outputPath, inplace)
	if err != nil {
		return err
	}

	targetBytes, err := optimizer.ParseSize(targetSizeStr)
	if err != nil {
		return fmt.Errorf("invalid target size: %w", err)
	}

	config := optimizer.Config{
		InputPath:     inputFile,
		OutputPath:    output,
		TargetBytes:   targetBytes,
		Quality:       quality,
		StripMetadata: stripMetadata,
		Silent:        silent,
		Verbose:       verbose,
		SkipPostOptim: skipPostOptim,
	}

	opt := optimizer.New(config)
	result, err := opt.Optimize()
	if err != nil {
		return fmt.Errorf("optimization failed: %w", err)
	}

	if !silent {
		fmt.Printf("✓ Optimized: %s → %s\n", inputFile, output)
		fmt.Printf("  Original size: %s\n", formatBytes(result.OriginalSize))
		fmt.Printf("  Final size: %s\n", formatBytes(result.FinalSize))
		fmt.Printf("  Scale: %d%%\n", result.FinalScale)
		fmt.Printf("  Iterations: %d\n", result.Iterations)
		if len(result.OptimizersUsed) > 0 {
			fmt.Printf("  Optimizers: %s\n", strings.Join(result.OptimizersUsed, ", "))
		}
		if result.MetadataStripped {
			fmt.Printf("  Metadata: stripped\n")
		}
	}

	return nil
}

// Shared output path resolution logic
func resolveOutputPath(inputFile, outputPath string, inplace bool) (string, error) {
	absInputFile, err := filepath.Abs(inputFile)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	if inplace {
		return inputFile, nil
	}

	if outputPath == "" {
		ext := filepath.Ext(inputFile)
		base := strings.TrimSuffix(inputFile, ext)
		return fmt.Sprintf("%s_optimized%s", base, ext), nil
	}

	if strings.HasPrefix(outputPath, ":") {
		inputDir := filepath.Dir(absInputFile)
		relativeOutput := strings.TrimPrefix(outputPath, ":")
		return filepath.Join(inputDir, relativeOutput), nil
	}

	if isFormatOnly(outputPath) {
		ext := filepath.Ext(inputFile)
		base := strings.TrimSuffix(inputFile, ext)
		newExt := normalizeExtension(outputPath)
		return fmt.Sprintf("%s_optimized.%s", base, newExt), nil
	}

	return outputPath, nil
}

func isFormatOnly(output string) bool {
	normalized := strings.ToLower(strings.TrimSpace(output))
	if strings.Contains(normalized, "/") || strings.Contains(normalized, "\\") {
		return false
	}
	switch normalized {
	case "jpg", "jpeg", "png", "webp", "gif", "bmp", "tiff", "tif":
		return true
	}
	return false
}

func normalizeExtension(format string) string {
	normalized := strings.ToLower(strings.TrimSpace(format))
	return strings.TrimPrefix(normalized, ".")
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
