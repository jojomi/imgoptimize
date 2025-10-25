# imgoptimize

`imgoptimize` is a smart command-line image optimizer written in Go.  
It reduces image file sizes to meet a specific target (e.g. 200 KB) while keeping visual quality as high as possible.

The tool works by **iteratively scaling and compressing** images, using **binary search** to find the optimal size–scale balance automatically.  
It optionally removes metadata and applies post-processing optimizers like `pngquant`, `oxipng`, or `jpegoptim` for smaller final files.


## Key Features

- Automatic binary search for optimal scale (fast convergence)
- Precise control over target file size (e.g. `200k`, `1.5M`)
- Optional metadata cleanup using `mat2`
- Post-compression with `pngquant`/`jpegoptim`/`oxipng`
- Easily converts formats (e.g. `.jpg → .png`)
- Safe: reuses original only after successful optimization
- Simple output path handling (see `:` notation below)

## Requirements

You’ll need these command-line tools installed for best results:

- [`ImageMagick`](https://imagemagick.org) (`magick` or `convert`)
- Optional: `pngquant`, `oxipng`, `jpegoptim`, `mat2`

Install on Debian/Ubuntu:

```
sudo apt install imagemagick pngquant oxipng jpegoptim mat2
```

## Usage

### Basic optimization

```
imgoptimize scale-down-to-filesize 200k photo.jpg
```

Creates `photo_optimized.jpg` under 200 KB.

### Convert formats

```
imgoptimize scale-down-to-filesize 500k logo.jpg --output png
```

```
imgoptimize scale-down-to-filesize 500k logo.jpg --output input.png
```

Uses `.png` extension to determine output format.

### Works with absolute and relative paths

```
imgoptimize scale-down-to-filesize 150k ./input/image.png --output ./optimized/image.png
```

Or use the **colon (`:`)** prefix to make the output path relative to the input file’s directory:

```
imgoptimize scale-down-to-filesize 150k /tmp/image.png --output :resized.png
```

Output: /tmp/resized.png

### With metadata stripping

```
imgoptimize scale-down-to-filesize 200k photo.jpg --strip-metadata
```

Removes EXIF and other metadata using `mat2`.

### With quiet mode and custom quality

```
imgoptimize scale-down-to-filesize 300k input.png --quality 90 --silent
```

Runs silently and uses fixed quality for compression.

## Output Example

```
Iteration 1: trying scale=70% (range: 1%-100%)
→ size=310.2 KiB ✗ (too large, trying smaller)
Iteration 2: trying scale=55% (range: 1%-69%)
→ size=198.7 KiB ✓ (under target, trying larger)
✓ Optimized: input.png → input_optimized.png
Original size: 1.2 MiB
Final size: 198.7 KiB
Scale: 55%
Iterations: 5
Optimizers: pngquant, mat2
Metadata: stripped
```

## Output Path Rules

- Default output: `<input>_optimized.<ext>`
- `--output` sets the destination manually
- `--output :file.jpg` → stores output in same directory as input
- `--inplace` modifies the input file directly

## License

MIT License © 2025