#!/usr/bin/env bash
set -u
export LC_ALL=C

SCRIPT_VERSION="0.2.2"

print_usage() {
  cat <<'USAGE'
Usage: bash benchmark.sh [options]

Benchmark profile (default):
  Input: video_720.mp4 (auto-generated if missing)
  Clip duration: 5 seconds per test
  Codecs: h264,h265,av1

Options:
  --inputs CSV         Comma-separated input files
  --codecs CSV         Comma-separated codecs: h264,h265,av1 (hevc alias supported)
  --duration-sec N     Clip duration in seconds per test (default: 5)
  --outdir DIR         Output directory (default: ./bench_out)
  --preset NAME        Preset for h264/h265 (default: medium)
  --crf-h264 N         CRF for h264 (default: 23)
  --crf-h265 N         CRF for h265 (default: 28)
  --crf-hevc N         Alias of --crf-h265
  --crf-av1 N          CRF/quality value for av1 (default: 32)
  --max-jobs N         Reserved for future parallel runs; currently only 1 is supported
  --keep-outputs       Keep encoded video outputs (default: remove encoded outputs)
  --json               Write results.json (enabled by default)
  --markdown           Write results.md (enabled by default)
  --no-json            Skip JSON output
  --no-markdown        Skip Markdown output
  --help               Show this help
USAGE
}

die() {
  printf 'Error: %s\n' "$1" >&2
  exit 1
}

print_ffmpeg_install_help() {
  cat <<'HELP' >&2
Install ffmpeg/ffprobe first, then re-run.

macOS:
  brew install ffmpeg

Ubuntu/Debian:
  sudo apt update && sudo apt install -y ffmpeg
HELP
}

trim() {
  printf '%s' "$1" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'
}

json_escape() {
  # Portable JSON string escaping (GNU/BSD awk compatible).
  printf '%s' "$1" | awk '
    BEGIN {
      ORS = ""
    }
    {
      gsub(/\\/, "\\\\")
      gsub(/"/, "\\\"")
      gsub(/\r/, "\\r")
      if (NR > 1) {
        printf "\\n"
      }
      printf "%s", $0
    }
  '
}

compact_multiline_for_tsv() {
  awk 'BEGIN { ORS="" } { gsub(/\t/, " "); if (NR > 1) printf "\\n"; printf "%s", $0 }'
}

detect_cpu_model() {
  local model
  model=""

  if command -v sysctl >/dev/null 2>&1; then
    model=$(sysctl -n machdep.cpu.brand_string 2>/dev/null || true)
  fi

  if [ -z "$model" ] && [ -r /proc/cpuinfo ]; then
    model=$(awk -F ':' '/model name/ {print $2; exit}' /proc/cpuinfo | sed 's/^[[:space:]]*//')
  fi

  if [ -z "$model" ]; then
    model="unknown"
  fi

  printf '%s' "$model"
}

detect_logical_cores() {
  local cores
  cores=""

  if command -v getconf >/dev/null 2>&1; then
    cores=$(getconf _NPROCESSORS_ONLN 2>/dev/null || true)
  fi

  if [ -z "$cores" ] && command -v sysctl >/dev/null 2>&1; then
    cores=$(sysctl -n hw.logicalcpu 2>/dev/null || true)
  fi

  if [ -z "$cores" ]; then
    cores="unknown"
  fi

  printf '%s' "$cores"
}

resolution_info() {
  local input="$1"
  local base
  local dims
  local height

  base=$(basename "$input")

  case "$base" in
    video.mp4)
      printf '4K\t0.50\n'
      return
      ;;
    video_1080.mp4)
      printf '1080p\t0.30\n'
      return
      ;;
    video_720.mp4)
      printf '720p\t0.20\n'
      return
      ;;
  esac

  if [ -f "$input" ]; then
    dims=$(ffprobe -v error -select_streams v:0 -show_entries stream=width,height -of csv=p=0:s=x "$input" 2>/dev/null || true)
    height=${dims#*x}

    if [ -n "$height" ] && [ "$height" -ge 2000 ] 2>/dev/null; then
      printf '>=4K\t0.50\n'
      return
    fi

    if [ -n "$height" ] && [ "$height" -ge 1000 ] 2>/dev/null; then
      printf '1080p\t0.30\n'
      return
    fi

    if [ -n "$height" ] && [ "$height" -ge 700 ] 2>/dev/null; then
      printf '720p\t0.20\n'
      return
    fi
  fi

  printf 'unknown\t0.20\n'
}

codec_weight() {
  case "$1" in
    h264)
      printf '0.20'
      ;;
    h265)
      printf '0.35'
      ;;
    av1)
      printf '0.45'
      ;;
    *)
      printf '0'
      ;;
  esac
}

run_timed_command() {
  local log_file="$1"
  shift

  if [ -x /usr/bin/time ]; then
    /usr/bin/time -p "$@" 1>/dev/null 2>"$log_file"
  else
    { time -p "$@" 1>/dev/null; } 2>"$log_file"
  fi
}

print_json_array() {
  local first
  local item
  local escaped

  first=1
  for item in "$@"; do
    escaped=$(json_escape "$item")
    if [ "$first" -eq 1 ]; then
      printf '"%s"' "$escaped"
      first=0
    else
      printf ', "%s"' "$escaped"
    fi
  done
}

generate_default_input_if_missing() {
  local input_path="$1"
  local clip_sec="$2"

  if [ -f "$input_path" ]; then
    return 0
  fi

  printf 'Default input not found: %s\n' "$input_path" >&2
  printf 'Generating synthetic 720p sample clip (%ss)...\n' "$clip_sec" >&2

  if ffmpeg -hide_banner -loglevel error -y \
      -f lavfi -i "testsrc2=size=1280x720:rate=30" \
      -t "$clip_sec" -pix_fmt yuv420p \
      -c:v mpeg4 -q:v 5 \
      "$input_path"; then
    return 0
  fi

  return 1
}

INPUTS=("video_720.mp4")
CODECS=("h264" "h265" "av1")
OUTDIR="./bench_out"
PRESET="medium"
CLIP_DURATION_SEC="5"
CRF_H264="23"
CRF_H265="28"
CRF_AV1="32"
MAX_JOBS="1"
KEEP_OUTPUTS=0
WRITE_JSON=1
WRITE_MARKDOWN=1
CUSTOM_INPUTS=0

while [ $# -gt 0 ]; do
  case "$1" in
    --inputs)
      [ $# -ge 2 ] || die "--inputs requires a value"
      IFS=',' read -r -a INPUTS <<< "$2"
      CUSTOM_INPUTS=1
      shift 2
      ;;
    --codecs)
      [ $# -ge 2 ] || die "--codecs requires a value"
      IFS=',' read -r -a CODECS <<< "$2"
      shift 2
      ;;
    --duration-sec)
      [ $# -ge 2 ] || die "--duration-sec requires a value"
      CLIP_DURATION_SEC="$2"
      shift 2
      ;;
    --outdir)
      [ $# -ge 2 ] || die "--outdir requires a value"
      OUTDIR="$2"
      shift 2
      ;;
    --preset)
      [ $# -ge 2 ] || die "--preset requires a value"
      PRESET="$2"
      shift 2
      ;;
    --crf-h264)
      [ $# -ge 2 ] || die "--crf-h264 requires a value"
      CRF_H264="$2"
      shift 2
      ;;
    --crf-h265|--crf-hevc)
      [ $# -ge 2 ] || die "$1 requires a value"
      CRF_H265="$2"
      shift 2
      ;;
    --crf-av1)
      [ $# -ge 2 ] || die "--crf-av1 requires a value"
      CRF_AV1="$2"
      shift 2
      ;;
    --max-jobs)
      [ $# -ge 2 ] || die "--max-jobs requires a value"
      MAX_JOBS="$2"
      shift 2
      ;;
    --keep-outputs)
      KEEP_OUTPUTS=1
      shift
      ;;
    --json)
      WRITE_JSON=1
      shift
      ;;
    --markdown)
      WRITE_MARKDOWN=1
      shift
      ;;
    --no-json)
      WRITE_JSON=0
      shift
      ;;
    --no-markdown)
      WRITE_MARKDOWN=0
      shift
      ;;
    --help|-h)
      print_usage
      exit 0
      ;;
    *)
      die "Unknown option: $1"
      ;;
  esac
done

if ! printf '%s' "$MAX_JOBS" | grep -Eq '^[0-9]+$'; then
  die "--max-jobs must be a positive integer"
fi

if [ "$MAX_JOBS" -lt 1 ]; then
  die "--max-jobs must be >= 1"
fi

if ! awk -v n="$CLIP_DURATION_SEC" 'BEGIN { exit !(n ~ /^[0-9]+([.][0-9]+)?$/ && n > 0) }'; then
  die "--duration-sec must be a positive number (e.g. 5 or 2.5)"
fi

if [ "$MAX_JOBS" -ne 1 ]; then
  printf 'Warning: --max-jobs currently supports only 1. Using 1.\n' >&2
  MAX_JOBS=1
fi

if [ "${#INPUTS[@]}" -eq 0 ]; then
  die "No inputs provided"
fi

if [ "${#CODECS[@]}" -eq 0 ]; then
  die "No codecs provided"
fi

clean_inputs=()
for input in "${INPUTS[@]}"; do
  input=$(trim "$input")
  if [ -n "$input" ]; then
    clean_inputs+=("$input")
  fi
done
INPUTS=("${clean_inputs[@]}")

clean_codecs=()
for codec in "${CODECS[@]}"; do
  canonical_codec=""
  codec=$(trim "$codec")
  codec=$(printf '%s' "$codec" | tr '[:upper:]' '[:lower:]')

  case "$codec" in
    h264|av1)
      canonical_codec="$codec"
      ;;
    h265|hevc)
      canonical_codec="h265"
      ;;
    *)
      die "Unsupported codec: $codec (allowed: h264, h265, av1)"
      ;;
  esac

  if [ "${#clean_codecs[@]}" -eq 0 ]; then
    clean_codecs=("$canonical_codec")
  else
    already_added=0
    for existing_codec in "${clean_codecs[@]}"; do
      if [ "$existing_codec" = "$canonical_codec" ]; then
        already_added=1
        break
      fi
    done

    if [ "$already_added" -eq 0 ]; then
      clean_codecs+=("$canonical_codec")
    fi
  fi
done
CODECS=("${clean_codecs[@]}")

if [ "${#INPUTS[@]}" -eq 0 ]; then
  die "No valid inputs provided"
fi

if [ "${#CODECS[@]}" -eq 0 ]; then
  die "No valid codecs provided"
fi

if ! command -v ffmpeg >/dev/null 2>&1; then
  printf 'Error: ffmpeg is not installed or not in PATH.\n' >&2
  print_ffmpeg_install_help
  exit 1
fi

if ! command -v ffprobe >/dev/null 2>&1; then
  printf 'Error: ffprobe is not installed or not in PATH.\n' >&2
  print_ffmpeg_install_help
  exit 1
fi

# For one-line default usage, auto-generate the expected sample input.
if [ "$CUSTOM_INPUTS" -eq 0 ] && [ "${#INPUTS[@]}" -eq 1 ] && [ "${INPUTS[0]}" = "video_720.mp4" ]; then
  if [ ! -f "${INPUTS[0]}" ]; then
    if ! generate_default_input_if_missing "${INPUTS[0]}" "$CLIP_DURATION_SEC"; then
      die "Could not generate default input file '${INPUTS[0]}'. Try running in a writable directory or pass --inputs with an existing file."
    fi
  fi
fi

mkdir -p "$OUTDIR" || die "Could not create output directory: $OUTDIR"
LOG_DIR="$OUTDIR/logs"
ENCODED_DIR="$OUTDIR/encoded"
mkdir -p "$LOG_DIR" "$ENCODED_DIR" || die "Could not create output subdirectories"

RAW_TSV="$OUTDIR/cases_raw.tsv"
SCORED_TSV="$OUTDIR/cases_scored.tsv"
JSON_FILE="$OUTDIR/results.json"
MD_FILE="$OUTDIR/results.md"

printf 'input\tcodec\tencoder\tres_label\tres_weight\tcodec_weight\traw_weight\tduration_sec\telapsed_sec\tspeed_x\tstatus\toutput_file\tstderr_tail\n' > "$RAW_TSV"

RUN_DATE_UTC=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
OS_NAME=$(uname -s 2>/dev/null || printf 'unknown')
OS_RELEASE=$(uname -r 2>/dev/null || printf 'unknown')
CPU_ARCH=$(uname -m 2>/dev/null || printf 'unknown')
CPU_MODEL=$(detect_cpu_model)
LOGICAL_CORES=$(detect_logical_cores)
FFMPEG_VERSION=$(ffmpeg -version 2>/dev/null | awk 'NR==1 {print; exit}')

ENCODERS_TEXT=$(ffmpeg -hide_banner -encoders 2>/dev/null || true)

has_encoder() {
  printf '%s\n' "$ENCODERS_TEXT" | grep -Eq "(^|[[:space:]])$1([[:space:]]|$)"
}

H264_ENCODER=""
H265_ENCODER=""
AV1_ENCODER=""

if has_encoder "libx264"; then
  H264_ENCODER="libx264"
fi

if has_encoder "libx265"; then
  H265_ENCODER="libx265"
fi

if has_encoder "libsvtav1"; then
  AV1_ENCODER="libsvtav1"
elif has_encoder "librav1e"; then
  AV1_ENCODER="librav1e"
fi

TOTAL_CASES=$(( ${#INPUTS[@]} * ${#CODECS[@]} ))
CASE_INDEX=0

printf 'Video benchmark started: %s\n' "$RUN_DATE_UTC"
printf 'System: %s %s | CPU: %s | Cores: %s\n' "$OS_NAME" "$OS_RELEASE" "$CPU_MODEL" "$LOGICAL_CORES"
printf 'ffmpeg: %s\n' "$FFMPEG_VERSION"
printf 'Clip duration per test: %ss\n' "$CLIP_DURATION_SEC"
printf 'Output dir: %s\n\n' "$OUTDIR"

for input in "${INPUTS[@]}"; do
  res_info=$(resolution_info "$input")
  res_label=$(printf '%s' "$res_info" | awk -F'\t' '{print $1}')
  res_weight=$(printf '%s' "$res_info" | awk -F'\t' '{print $2}')

  source_duration_sec="0"
  duration_sec="0"
  if [ -f "$input" ]; then
    probed_duration=$(ffprobe -v error -show_entries format=duration -of default=nokey=1:noprint_wrappers=1 "$input" 2>/dev/null | head -n 1)
    if awk -v d="$probed_duration" 'BEGIN { exit !(d > 0) }'; then
      source_duration_sec="$probed_duration"
    fi
  fi

  if awk -v d="$source_duration_sec" 'BEGIN { exit !(d > 0) }'; then
    duration_sec=$(awk -v src="$source_duration_sec" -v clip="$CLIP_DURATION_SEC" 'BEGIN { if (src < clip) printf "%.6f", src; else printf "%.6f", clip }')
  else
    duration_sec=$(awk -v clip="$CLIP_DURATION_SEC" 'BEGIN { printf "%.6f", clip }')
  fi

  for codec in "${CODECS[@]}"; do
    CASE_INDEX=$((CASE_INDEX + 1))

    codec_w=$(codec_weight "$codec")
    raw_weight=$(awk -v a="$res_weight" -v b="$codec_w" 'BEGIN { printf "%.12f", a * b }')

    status=""
    elapsed_sec="0"
    speed_x="0"
    stderr_tail=""
    output_file=""

    base_input=$(basename "$input")

    printf '[%d/%d] %s -> %s ... ' "$CASE_INDEX" "$TOTAL_CASES" "$base_input" "$codec"

    if [ ! -f "$input" ]; then
      status="FAILED"
      stderr_tail="Input file not found: $input"
    else
      encoder=""
      case "$codec" in
        h264)
          encoder="$H264_ENCODER"
          ;;
        h265)
          encoder="$H265_ENCODER"
          ;;
        av1)
          encoder="$AV1_ENCODER"
          ;;
      esac

      if [ -z "$encoder" ]; then
        status="UNSUPPORTED"
        stderr_tail="Encoder unavailable for codec $codec"
      else
        input_stem=${base_input%.*}
        output_file="$ENCODED_DIR/${input_stem}_${codec}.mp4"
        log_file="$LOG_DIR/${input_stem}_${codec}.log"

        codec_args=()
        case "$codec" in
          h264)
            codec_args=(-c:v "$encoder" -preset "$PRESET" -crf "$CRF_H264")
            ;;
          h265)
            codec_args=(-c:v "$encoder" -preset "$PRESET" -crf "$CRF_H265")
            ;;
          av1)
            if [ "$encoder" = "libsvtav1" ]; then
              codec_args=(-c:v "$encoder" -preset 6 -crf "$CRF_AV1")
            else
              codec_args=(-c:v "$encoder" -speed 6 -qp "$CRF_AV1")
            fi
            ;;
        esac

        if run_timed_command "$log_file" ffmpeg -hide_banner -loglevel error -y -i "$input" -t "$duration_sec" "${codec_args[@]}" -an "$output_file"; then
          cmd_status=0
        else
          cmd_status=$?
        fi

        elapsed_parsed=$(awk '$1 == "real" {print $2}' "$log_file" | tail -n 1)
        if [ -n "$elapsed_parsed" ]; then
          elapsed_sec="$elapsed_parsed"
        fi

        stderr_filtered=$(grep -vE '^(real|user|sys)[[:space:]]' "$log_file" || true)
        stderr_tail=$(printf '%s\n' "$stderr_filtered" | compact_multiline_for_tsv)

        if [ "$cmd_status" -eq 0 ]; then
          if awk -v e="$elapsed_sec" 'BEGIN { exit !(e > 0) }'; then
            speed_x=$(awk -v d="$duration_sec" -v e="$elapsed_sec" 'BEGIN { if (e > 0) printf "%.6f", d / e; else print "0" }')
            status="OK"
          else
            speed_x="0"
            status="FAILED"
            if [ -n "$stderr_tail" ]; then
              stderr_tail="$stderr_tail\\nCould not parse elapsed time"
            else
              stderr_tail="Could not parse elapsed time"
            fi
          fi
        else
          speed_x="0"
          status="FAILED"
        fi

        if [ "$KEEP_OUTPUTS" -eq 0 ]; then
          rm -f "$output_file"
          output_file=""
        fi
      fi
    fi

    speed_display=$(awk -v s="$speed_x" 'BEGIN { printf "%.2fx", s + 0 }')
    elapsed_display=$(awk -v e="$elapsed_sec" 'BEGIN { printf "%.2fs", e + 0 }')
    printf '%s (%s, %s)\n' "$status" "$speed_display" "$elapsed_display"

    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
      "$input" "$codec" "${encoder:-}" "$res_label" "$res_weight" "$codec_w" "$raw_weight" "$duration_sec" "$elapsed_sec" "$speed_x" "$status" "$output_file" "$stderr_tail" >> "$RAW_TSV"
  done
done

TOTAL_RAW_WEIGHT=$(awk -F'\t' 'NR > 1 {sum += $7} END { if (sum > 0) printf "%.12f", sum; else print "1" }' "$RAW_TSV")

awk -F'\t' -v OFS='\t' -v total_raw="$TOTAL_RAW_WEIGHT" '
NR == 1 {
  print $0, "weight_norm", "case_score"
  next
}
{
  weight_norm = (total_raw > 0) ? ($7 / total_raw) : 0
  normalized_speed = $10 / 5.0
  if (normalized_speed > 1.0) normalized_speed = 1.0
  if ($11 != "OK") normalized_speed = 0
  case_score = weight_norm * normalized_speed

  printf "%s\t%s\t%s\t%s\t%.6f\t%.6f\t%.12f\t%.6f\t%.6f\t%.6f\t%s\t%s\t%s\t%.12f\t%.12f\n", \
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, weight_norm, case_score
}
' "$RAW_TSV" > "$SCORED_TSV"

TOTAL_SCORE=$(awk -F'\t' 'NR > 1 {sum += $15} END { printf "%d", (sum * 1000) + 0.5 }' "$SCORED_TSV")
SUCCESSFUL_CASES=$(awk -F'\t' 'NR > 1 && $11 == "OK" {c++} END { print c + 0 }' "$SCORED_TSV")
FAILED_CASES=$(awk -F'\t' 'NR > 1 && $11 == "FAILED" {c++} END { print c + 0 }' "$SCORED_TSV")
UNSUPPORTED_CASES=$(awk -F'\t' 'NR > 1 && $11 == "UNSUPPORTED" {c++} END { print c + 0 }' "$SCORED_TSV")
GEOMEAN_SPEED=$(awk -F'\t' 'NR > 1 && $11 == "OK" && $10 > 0 {sum += log($10); n++} END { if (n > 0) printf "%.4f", exp(sum / n); else print "0.0000" }' "$SCORED_TSV")

printf '\nCase results:\n'
printf '%-16s %-6s %-11s %-9s %-9s %-8s %-8s\n' "Input" "Codec" "Status" "Speed" "Elapsed" "Weight" "Score"
printf '%s\n' "---------------------------------------------------------------------------"
awk -F'\t' '
NR > 1 {
  input = $1
  gsub(/^.*\//, "", input)
  printf "%-16s %-6s %-11s %-9s %-9s %-8s %-8s\n", \
    input, $2, $11, sprintf("%.2fx", $10 + 0), sprintf("%.2fs", $9 + 0), sprintf("%.3f", $14 + 0), sprintf("%.3f", $15 + 0)
}
' "$SCORED_TSV"

printf '\nCodec ranking (by speed):\n'
printf '%-4s %-6s %-11s %-9s %-9s %-9s\n' "Rank" "Codec" "Status" "Speed" "Elapsed" "Score"
printf '%s\n' "--------------------------------------------------------------"
awk -F'\t' '
NR > 1 {
  printf "%s\t%s\t%.6f\t%.6f\t%.6f\n", $2, $11, $10 + 0, $9 + 0, $15 + 0
}
' "$SCORED_TSV" | sort -t "$(printf '\t')" -k3,3nr | awk -F'\t' '
{
  printf "%-4d %-6s %-11s %-9s %-9s %-9s\n", NR, $1, $2, sprintf("%.2fx", $3 + 0), sprintf("%.2fs", $4 + 0), sprintf("%.3f", $5 + 0)
}
'

printf '\nSummary:\n'
printf '  Successful cases:  %s\n' "$SUCCESSFUL_CASES"
printf '  Failed cases:      %s\n' "$FAILED_CASES"
printf '  Unsupported cases: %s\n' "$UNSUPPORTED_CASES"
printf '  Geomean speed:     %sx\n' "$GEOMEAN_SPEED"
printf '  Total score:       %s / 1000\n' "$TOTAL_SCORE"

if [ "$WRITE_JSON" -eq 1 ]; then
  {
    printf '{\n'
    printf '  "script_version": "%s",\n' "$(json_escape "$SCRIPT_VERSION")"
    printf '  "run_date_utc": "%s",\n' "$(json_escape "$RUN_DATE_UTC")"
    printf '  "system": {\n'
    printf '    "os": "%s",\n' "$(json_escape "$OS_NAME")"
    printf '    "os_release": "%s",\n' "$(json_escape "$OS_RELEASE")"
    printf '    "cpu_arch": "%s",\n' "$(json_escape "$CPU_ARCH")"
    printf '    "cpu_model": "%s",\n' "$(json_escape "$CPU_MODEL")"
    printf '    "logical_cores": "%s",\n' "$(json_escape "$LOGICAL_CORES")"
    printf '    "ffmpeg_version": "%s"\n' "$(json_escape "$FFMPEG_VERSION")"
    printf '  },\n'
    printf '  "run_config": {\n'
    printf '    "inputs": ['
    print_json_array "${INPUTS[@]}"
    printf '],\n'
    printf '    "codecs": ['
    print_json_array "${CODECS[@]}"
    printf '],\n'
    printf '    "clip_duration_sec": %.3f,\n' "$CLIP_DURATION_SEC"
    printf '    "preset": "%s",\n' "$(json_escape "$PRESET")"
    printf '    "crf_h264": %s,\n' "$CRF_H264"
    printf '    "crf_h265": %s,\n' "$CRF_H265"
    printf '    "crf_av1": %s,\n' "$CRF_AV1"
    printf '    "max_jobs": %s\n' "$MAX_JOBS"
    printf '  },\n'
    printf '  "cases": [\n'

    awk -F'\t' '
      function esc(s) {
        gsub(/\\/, "\\\\", s)
        gsub(/"/, "\\\"", s)
        gsub(/\r/, "\\r", s)
        gsub(/\n/, "\\n", s)
        return s
      }

      NR == 1 {
        next
      }

      {
        if (count > 0) {
          printf "    ,\n"
        }

        printf "    {\n"
        printf "      \"input\": \"%s\",\n", esc($1)
        printf "      \"codec\": \"%s\",\n", esc($2)
        printf "      \"encoder\": \"%s\",\n", esc($3)
        printf "      \"resolution_label\": \"%s\",\n", esc($4)
        printf "      \"duration_sec\": %.6f,\n", $8 + 0
        printf "      \"elapsed_sec\": %.6f,\n", $9 + 0
        printf "      \"speed_x\": %.6f,\n", $10 + 0
        printf "      \"status\": \"%s\",\n", esc($11)
        printf "      \"weight_norm\": %.12f,\n", $14 + 0
        printf "      \"case_score\": %.12f,\n", $15 + 0
        printf "      \"output_file\": \"%s\",\n", esc($12)
        printf "      \"stderr_tail\": \"%s\"\n", esc($13)
        printf "    }\n"

        count++
      }
    ' "$SCORED_TSV"

    printf '  ],\n'
    printf '  "summary": {\n'
    printf '    "successful_cases": %s,\n' "$SUCCESSFUL_CASES"
    printf '    "failed_cases": %s,\n' "$FAILED_CASES"
    printf '    "unsupported_cases": %s,\n' "$UNSUPPORTED_CASES"
    printf '    "geomean_speed_x": %.6f,\n' "$GEOMEAN_SPEED"
    printf '    "total_score_1000": %s\n' "$TOTAL_SCORE"
    printf '  }\n'
    printf '}\n'
  } > "$JSON_FILE"
fi

if [ "$WRITE_MARKDOWN" -eq 1 ]; then
  {
    printf '# Video Encode Benchmark Results\n\n'
    printf -- '- Run date (UTC): `%s`\n' "$RUN_DATE_UTC"
    printf -- '- Script version: `%s`\n' "$SCRIPT_VERSION"
    printf -- '- Clip duration per test: `%ss`\n' "$CLIP_DURATION_SEC"
    printf -- '- Total score: **%s / 1000**\n' "$TOTAL_SCORE"
    printf -- '- Geomean speed (OK cases): **%sx**\n\n' "$GEOMEAN_SPEED"

    printf '## System\n\n'
    printf '| Key | Value |\n'
    printf '|---|---|\n'
    printf '| OS | `%s %s` |\n' "$OS_NAME" "$OS_RELEASE"
    printf '| CPU | `%s` |\n' "$CPU_MODEL"
    printf '| Logical cores | `%s` |\n' "$LOGICAL_CORES"
    printf '| ffmpeg | `%s` |\n\n' "$FFMPEG_VERSION"

    printf '## Cases\n\n'
    printf '| Input | Codec | Encoder | Status | Duration (s) | Elapsed (s) | Speed (x) | Weight | Case Score |\n'
    printf '|---|---|---|---|---:|---:|---:|---:|---:|\n'

    awk -F'\t' '
      NR == 1 {
        next
      }

      {
        input = $1
        sub(/^.*\//, "", input)
        encoder = $3
        if (encoder == "") {
          encoder = "n/a"
        }

        printf "| `%s` | `%s` | `%s` | `%s` | %.3f | %.3f | %.3f | %.4f | %.4f |\n", \
          input, $2, encoder, $11, $8 + 0, $9 + 0, $10 + 0, $14 + 0, $15 + 0
      }
    ' "$SCORED_TSV"

    printf '\n## Ranking (By Speed)\n\n'
    printf '| Rank | Codec | Status | Speed (x) | Elapsed (s) | Case Score |\n'
    printf '|---:|---|---|---:|---:|---:|\n'
    awk -F'\t' '
      NR > 1 {
        printf "%s\t%s\t%.6f\t%.6f\t%.6f\n", $2, $11, $10 + 0, $9 + 0, $15 + 0
      }
    ' "$SCORED_TSV" | sort -t "$(printf '\t')" -k3,3nr | awk -F'\t' '
      {
        printf "| %d | `%s` | `%s` | %.3f | %.3f | %.4f |\n", NR, $1, $2, $3 + 0, $4 + 0, $5 + 0
      }
    '

    printf '\n## Summary\n\n'
    printf -- '- Successful cases: `%s`\n' "$SUCCESSFUL_CASES"
    printf -- '- Failed cases: `%s`\n' "$FAILED_CASES"
    printf -- '- Unsupported cases: `%s`\n' "$UNSUPPORTED_CASES"

    if [ "$FAILED_CASES" -gt 0 ] || [ "$UNSUPPORTED_CASES" -gt 0 ]; then
      printf '\n## Non-OK Cases\n\n'
      awk -F'\t' '
        NR == 1 {
          next
        }

        $11 != "OK" {
          input = $1
          sub(/^.*\//, "", input)
          msg = $13
          gsub(/\|/, "\\|", msg)

          if (msg != "") {
            printf "- `%s + %s`: `%s` (`%s`)\n", input, $2, $11, msg
          } else {
            printf "- `%s + %s`: `%s`\n", input, $2, $11
          }
        }
      ' "$SCORED_TSV"
    fi
  } > "$MD_FILE"
fi

if [ "$KEEP_OUTPUTS" -eq 0 ]; then
  rm -rf "$ENCODED_DIR"
fi

printf '\nWrote files:\n'
printf '  %s\n' "$SCORED_TSV"
if [ "$WRITE_JSON" -eq 1 ]; then
  printf '  %s\n' "$JSON_FILE"
fi
if [ "$WRITE_MARKDOWN" -eq 1 ]; then
  printf '  %s\n' "$MD_FILE"
fi
