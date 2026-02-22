# Video Encode Benchmark (Mac/Linux)

A single-script benchmark for comparing video encoding speed across machines.

Default profile:
- Input: `video_720.mp4`
- Clip duration per test: `5s`
- Codecs: `h264`, `h265`, `av1`

The script measures realtime throughput (`speed_x`) for each encode and computes a weighted total score (`0-1000`).

## Requirements

- `bash`
- `ffmpeg` and `ffprobe` installed in `PATH`

This benchmark always uses your system `ffmpeg` (no bundling, no auto-install).

## Quick Start

From a folder that contains `video_720.mp4`:

```bash
bash benchmark.sh
```

Output files are written to `./bench_out`:
- `cases_scored.tsv`
- `results.json`
- `results.md`
- per-case logs under `bench_out/logs/`

## One-Line Remote Run (when hosted)

Latest:

```bash
curl -fsSL https://raw.githubusercontent.com/<owner>/<repo>/main/benchmark.sh | bash
```

Pinned version:

```bash
curl -fsSL https://raw.githubusercontent.com/<owner>/<repo>/v0.2.1/benchmark.sh | bash
```

## CLI Options

```text
--inputs CSV         Comma-separated input files
--codecs CSV         h264,h265,av1 (hevc alias supported)
--duration-sec N     Clip duration in seconds per test (default: 5)
--outdir DIR         Output directory (default: ./bench_out)
--preset NAME        Preset for h264/h265 (default: medium)
--crf-h264 N         CRF for h264 (default: 23)
--crf-h265 N         CRF for h265 (default: 28)
--crf-hevc N         Alias of --crf-h265
--crf-av1 N          CRF/quality value for av1 (default: 32)
--max-jobs N         Reserved for future parallel runs; currently only 1 is supported
--keep-outputs       Keep encoded output videos
--json / --no-json
--markdown / --no-markdown
--help
```

Example:

```bash
bash benchmark.sh \
  --inputs "video_720.mp4" \
  --codecs "h264,h265,av1" \
  --duration-sec 5 \
  --outdir ./bench_out
```

## Scoring Model

Resolution weights:
- 4K: `0.50`
- 1080p: `0.30`
- 720p: `0.20` (default profile uses this only)

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

Failed/unsupported cases contribute `0` score and are still included in the report.
