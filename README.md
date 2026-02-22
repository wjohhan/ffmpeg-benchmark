# ffmpeg-benchmark

Cross-platform video encode benchmark as a single binary for macOS/Linux.

Default profile:
- Resolution: `720` -> `video_720.mp4` (auto-generated if missing)
- Clip duration per test: `5s`
- Codecs: `h264`, `h265`, `av1`

The benchmark uses your system `ffmpeg`/`ffprobe` and outputs:
- pretty console summary
- `bench_out/cases_scored.tsv`
- `bench_out/results.json`
- `bench_out/results.md`

## Requirements

- `ffmpeg` and `ffprobe` in `PATH`

## Install (Recommended)

Install latest release binary:

```bash
curl -fsSL https://raw.githubusercontent.com/wjohhan/ffmpeg-benchmark/main/install.sh | bash
```

Then run:

```bash
ffmpeg-benchmark run
```

Legacy command still works (wrapper):

```bash
curl -fsSL https://raw.githubusercontent.com/wjohhan/ffmpeg-benchmark/main/benchmark.sh | bash
```

## Build from source

```bash
git clone https://github.com/wjohhan/ffmpeg-benchmark.git
cd ffmpeg-benchmark
make build
./ffmpeg-benchmark run
```

## CLI

```text
ffmpeg-benchmark [run] [options]

--inputs, -i CSV     Comma-separated input files
--resolution, -r CSV Resolution preset(s): 720,1080,4k,all (default: 720)
--codecs, -c CSV     Comma-separated codecs: h264,h265,av1 (hevc alias supported)
--duration-sec, -d N Clip duration in seconds per test (default: 5)
--outdir, -o DIR     Output directory (default: ./bench_out)
--preset, -p NAME    Preset for h264/h265 (default: medium)
--crf-h264 N         CRF for h264 (default: 23)
--crf-h265 N         CRF for h265 (default: 28)
--crf-hevc N         Alias of --crf-h265
--crf-av1 N          CRF/quality value for av1 (default: 32)
--max-jobs, -j N     Reserved for future parallel runs; currently only 1 is supported
--keep-outputs, -k   Keep encoded output videos
--json               Write results.json (enabled by default)
--markdown           Write results.md (enabled by default)
--no-json            Skip JSON output
--no-markdown        Skip Markdown output
--version, -v        Print version
--help, -h           Show help

Notes:
- `--inputs` has priority over `--resolution`
- Missing preset files are auto-generated for non-custom input mode
- If `--duration-sec` is longer than the input file, the input is looped to match requested duration
```

Example:

```bash
ffmpeg-benchmark run \
  -r "1080" \
  -c "h264,h265,av1" \
  -d 5 \
  -o ./bench_out
```

All resolutions:

```bash
ffmpeg-benchmark run -r all -d 3
```

## Scoring model

Resolution weights:
- 4K: `0.50`
- 1080p: `0.30`
- 720p: `0.20`

Codec weights:
- AV1: `0.45`
- H265: `0.35`
- H264: `0.20`

Per case:
- `speed_x = duration_sec / elapsed_sec`
- `normalized_speed = min(speed_x / 5.0, 1.0)`
- `case_score = normalized_case_weight * normalized_speed`

Total:
- `total_score_1000 = round(1000 * sum(case_score))`

## Release build

Create release assets for Linux/macOS amd64+arm64:

```bash
make dist
```

Artifacts are written to `dist/`.
