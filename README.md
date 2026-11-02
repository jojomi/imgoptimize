# imgoptimize

`imgoptimize` is a smart command-line image optimizer written in Go.  
It reduces image file sizes to meet a specific target (e.g. 200 KB) while keeping visual quality as high as possible.

The tool works by **iteratively scaling and compressing** images, using **binary search** to find the optimal size–scale balance automatically.  
It always removes metadata and applies post-processing optimizers like `pngquant`, `oxipng`, `zopflipng`, or `jpegoptim` for smaller final files.


## Key Features

- Automatic binary search for optimal scale (fast convergence)
- Precise control over target file size (e.g. `200k`, `1.5M`)
- Always strips metadata (EXIF, GPS, XMP, ICC)
- Post-compression with `pngquant`/`jpegoptim`/`oxipng`/`zopflipng`/`gifsicle`
- JPEG via `mozjpeg`, progressive with 4:2:0 chroma subsampling
- AVIF at the slowest, most thorough encoder speed
- WebP: encodes lossy and lossless and keeps the smaller file (graphics and screenshots often end up lossless and several times smaller)
- Easily converts formats (e.g. `.jpg → .png`)
- Safe: reuses original only after successful optimization
- Simple output path handling (see `:` notation below)

## Requirements

Only ImageMagick is required. Every other tool is optional: if it is missing, that step is skipped and the output gets larger (or, for AVIF, may look worse).

| Tool | Required | Used for | macOS (Homebrew) | Debian/Ubuntu |
|---|---|---|---|---|
| [ImageMagick](https://imagemagick.org) (`magick` or `convert`) | **yes** | reading, resizing and encoding all formats; WebP and AVIF need its libwebp/libheif support | `imagemagick` | `imagemagick` |
| [mozjpeg](https://github.com/mozilla/mozjpeg) (`cjpeg`) | no | JPEG: about 20 % smaller than ImageMagick at the same quality | `mozjpeg` ¹ | not packaged, build from source ² |
| [jpegoptim](https://github.com/tjko/jpegoptim) | no | JPEG: lossless post-optimization | `jpegoptim` | `jpegoptim` |
| [pngquant](https://pngquant.org) | no | PNG: lossy palette reduction | `pngquant` | `pngquant` |
| [oxipng](https://github.com/shssoichiro/oxipng) | no | PNG: lossless recompression | `oxipng` | not packaged, `cargo install oxipng` ³ |
| [zopflipng](https://github.com/google/zopfli) | no | PNG: lossless final recompression | `zopfli` | `zopfli` |
| [gifsicle](https://www.lcdf.org/gifsicle/) | no | GIF: lossless recompression, about half the size for animations | `gifsicle` | `gifsicle` |
| [avifenc](https://github.com/AOMediaCodec/libavif) | no | AVIF with transparency: lossless alpha; without it transparent images may show a grey haze on dark backgrounds | `libavif` | `libavif-bin` |
| [exiftool](https://exiftool.org) | no (tests only) | checks in the test suite that metadata is removed | `exiftool` | `libimage-exiftool-perl` |

¹ Homebrew installs mozjpeg keg-only (not on `PATH`, because it conflicts with libjpeg-turbo). `imgoptimize` finds it in the Homebrew keg anyway.

² `imgoptimize` finds `cjpeg` on `PATH` or in `/opt/mozjpeg/bin` (the default install location of a source build). A `cjpeg` from libjpeg-turbo is ignored.

³ Debian only ships the Rust library (`librust-oxipng-dev`), not the program. `cargo install oxipng` needs `cargo` (`sudo apt install cargo`) and puts the binary into `~/.cargo/bin`, which must be on `PATH`.

Install everything on macOS:

```
brew install imagemagick mozjpeg jpegoptim pngquant oxipng zopfli gifsicle libavif
```

Install everything on Debian/Ubuntu (mozjpeg and oxipng see notes above):

```
sudo apt install imagemagick jpegoptim pngquant zopfli gifsicle libavif-bin
cargo install oxipng
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
Optimizers: pngquant, oxipng, zopflipng
```

## Output Path Rules

- Default output: `<input>_optimized.<ext>`
- `--output` sets the destination manually
- `--output :file.jpg` → stores output in same directory as input
- `--inplace` modifies the input file directly

## License

MIT License © 2025