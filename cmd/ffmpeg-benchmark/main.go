package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var Version = "0.3.3"

var errShowHelp = errors.New("show help")

type Config struct {
	Inputs       []string
	Resolution   string
	Codecs       []string
	DurationSec  float64
	OutDir       string
	Preset       string
	CRFH264      int
	CRFH265      int
	CRFAV1       int
	MaxJobs      int
	KeepOutputs  bool
	WriteJSON    bool
	WriteMD      bool
	CustomInputs bool
}

type SystemInfo struct {
	OS           string `json:"os"`
	OSRelease    string `json:"os_release"`
	CPUArch      string `json:"cpu_arch"`
	CPUModel     string `json:"cpu_model"`
	LogicalCores int    `json:"logical_cores"`
	FFmpeg       string `json:"ffmpeg_version"`
}

type RunConfig struct {
	Inputs          []string `json:"inputs"`
	Resolution      string   `json:"resolution"`
	Codecs          []string `json:"codecs"`
	ClipDurationSec float64  `json:"clip_duration_sec"`
	Preset          string   `json:"preset"`
	CRFH264         int      `json:"crf_h264"`
	CRFH265         int      `json:"crf_h265"`
	CRFAV1          int      `json:"crf_av1"`
	MaxJobs         int      `json:"max_jobs"`
}

type CaseResult struct {
	Input            string  `json:"input"`
	Codec            string  `json:"codec"`
	Encoder          string  `json:"encoder"`
	ResolutionLabel  string  `json:"resolution_label"`
	DurationSec      float64 `json:"duration_sec"`
	ElapsedSec       float64 `json:"elapsed_sec"`
	SpeedX           float64 `json:"speed_x"`
	Status           string  `json:"status"`
	WeightNorm       float64 `json:"weight_norm"`
	CaseScore        float64 `json:"case_score"`
	OutputFile       string  `json:"output_file"`
	StderrTail       string  `json:"stderr_tail"`
	ResolutionWeight float64 `json:"-"`
	CodecWeight      float64 `json:"-"`
	RawWeight        float64 `json:"-"`
}

type Summary struct {
	SuccessfulCases  int     `json:"successful_cases"`
	FailedCases      int     `json:"failed_cases"`
	UnsupportedCases int     `json:"unsupported_cases"`
	GeomeanSpeedX    float64 `json:"geomean_speed_x"`
	TotalScore1000   int     `json:"total_score_1000"`
}

type Results struct {
	ScriptVersion string       `json:"script_version"`
	RunDateUTC    string       `json:"run_date_utc"`
	System        SystemInfo   `json:"system"`
	RunConfig     RunConfig    `json:"run_config"`
	Cases         []CaseResult `json:"cases"`
	Summary       Summary      `json:"summary"`
}

type EncoderAvailability struct {
	H264 string
	H265 string
	AV1  string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, errShowHelp) {
			printUsage()
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, showVersion, err := parseConfig(args)
	if err != nil {
		return err
	}
	if showVersion {
		fmt.Println(Version)
		return nil
	}

	if cfg.MaxJobs != 1 {
		fmt.Fprintf(os.Stderr, "Warning: --max-jobs currently supports only 1. Using 1.\n")
		cfg.MaxJobs = 1
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return errors.New("ffmpeg is not installed or not in PATH")
	}

	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return errors.New("ffprobe is not installed or not in PATH")
	}

	if !cfg.CustomInputs {
		for _, input := range cfg.Inputs {
			if _, statErr := os.Stat(input); errors.Is(statErr, os.ErrNotExist) {
				label, width, height, ok := syntheticInputSpec(input)
				if !ok {
					continue
				}
				fmt.Fprintf(os.Stderr, "Input not found: %s\n", input)
				fmt.Fprintf(os.Stderr, "Generating synthetic %s sample clip (%ss)...\n", label, formatDurationValue(cfg.DurationSec))
				if err := generateSyntheticInput(ffmpegPath, input, width, height, cfg.DurationSec); err != nil {
					return fmt.Errorf("could not generate input %s: %w", input, err)
				}
			}
		}
	}

	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return fmt.Errorf("could not create output directory: %w", err)
	}

	logDir := filepath.Join(cfg.OutDir, "logs")
	encodedDir := filepath.Join(cfg.OutDir, "encoded")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("could not create log directory: %w", err)
	}
	if err := os.MkdirAll(encodedDir, 0o755); err != nil {
		return fmt.Errorf("could not create encoded directory: %w", err)
	}

	system := collectSystemInfo(ffmpegPath)
	runDateUTC := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	encoders, err := detectEncoders(ffmpegPath)
	if err != nil {
		return err
	}

	totalCases := len(cfg.Inputs) * len(cfg.Codecs)
	caseIndex := 0
	cases := make([]CaseResult, 0, totalCases)

	fmt.Printf("Video benchmark started: %s\n", runDateUTC)
	fmt.Printf("System: %s %s | CPU: %s | Cores: %d\n", system.OS, system.OSRelease, system.CPUModel, system.LogicalCores)
	fmt.Printf("ffmpeg: %s\n", system.FFmpeg)
	fmt.Printf("Clip duration per test: %ss\n", formatDurationValue(cfg.DurationSec))
	fmt.Printf("Output dir: %s\n\n", cfg.OutDir)

	for _, input := range cfg.Inputs {
		resLabel, resWeight := resolutionInfo(ffprobePath, input)
		sourceDuration := probeDuration(ffprobePath, input)
		testDuration := cfg.DurationSec
		loopInput := sourceDuration > 0 && sourceDuration < testDuration

		for _, codec := range cfg.Codecs {
			caseIndex++

			c := CaseResult{
				Input:            input,
				Codec:            codec,
				Status:           "FAILED",
				ResolutionLabel:  resLabel,
				ResolutionWeight: resWeight,
				CodecWeight:      codecWeight(codec),
				DurationSec:      testDuration,
			}
			c.RawWeight = c.ResolutionWeight * c.CodecWeight

			baseInput := filepath.Base(input)
			fmt.Printf("[%d/%d] %s -> %s ... ", caseIndex, totalCases, baseInput, codec)

			if _, statErr := os.Stat(input); errors.Is(statErr, os.ErrNotExist) {
				c.Status = "FAILED"
				c.StderrTail = fmt.Sprintf("Input file not found: %s", input)
				fmt.Printf("%s (%s, %s)\n", c.Status, formatSpeed(c.SpeedX), formatSeconds(c.ElapsedSec))
				cases = append(cases, c)
				continue
			}

			encoder := chooseEncoder(codec, encoders)
			c.Encoder = encoder
			if encoder == "" {
				c.Status = "UNSUPPORTED"
				c.StderrTail = fmt.Sprintf("Encoder unavailable for codec %s", codec)
				fmt.Printf("%s (%s, %s)\n", c.Status, formatSpeed(c.SpeedX), formatSeconds(c.ElapsedSec))
				cases = append(cases, c)
				continue
			}

			stem := strings.TrimSuffix(baseInput, filepath.Ext(baseInput))
			outputPath := filepath.Join(encodedDir, fmt.Sprintf("%s_%s.mp4", stem, codec))
			logPath := filepath.Join(logDir, fmt.Sprintf("%s_%s.log", stem, codec))

			stderrText, elapsed, ffArgs, runErr := runEncode(ffmpegPath, input, outputPath, codec, encoder, cfg, testDuration, loopInput)
			c.ElapsedSec = elapsed.Seconds()
			c.OutputFile = outputPath
			c.StderrTail = compactLog(stderrText)
			_ = writeCaseLog(logPath, ffArgs, c.ElapsedSec, stderrText)

			if runErr != nil {
				c.Status = "FAILED"
				if c.StderrTail == "" {
					c.StderrTail = runErr.Error()
				}
			} else if c.ElapsedSec <= 0 {
				c.Status = "FAILED"
				c.StderrTail = appendMsg(c.StderrTail, "Could not parse elapsed time")
			} else {
				c.Status = "OK"
				c.SpeedX = testDuration / c.ElapsedSec
			}

			if !cfg.KeepOutputs {
				_ = os.Remove(outputPath)
				c.OutputFile = ""
			}

			fmt.Printf("%s (%s, %s)\n", c.Status, formatSpeed(c.SpeedX), formatSeconds(c.ElapsedSec))
			cases = append(cases, c)
		}
	}

	scoreCases(cases)
	summary := summarize(cases)

	scoredTSV := filepath.Join(cfg.OutDir, "cases_scored.tsv")
	if err := writeScoredTSV(scoredTSV, cases); err != nil {
		return err
	}

	printCaseTable(cases)
	printRanking(cases)
	printSummary(summary)

	results := Results{
		ScriptVersion: Version,
		RunDateUTC:    runDateUTC,
		System:        system,
		RunConfig: RunConfig{
			Inputs:          cfg.Inputs,
			Resolution:      cfg.Resolution,
			Codecs:          cfg.Codecs,
			ClipDurationSec: cfg.DurationSec,
			Preset:          cfg.Preset,
			CRFH264:         cfg.CRFH264,
			CRFH265:         cfg.CRFH265,
			CRFAV1:          cfg.CRFAV1,
			MaxJobs:         cfg.MaxJobs,
		},
		Cases:   cases,
		Summary: summary,
	}

	jsonPath := filepath.Join(cfg.OutDir, "results.json")
	mdPath := filepath.Join(cfg.OutDir, "results.md")

	if cfg.WriteJSON {
		if err := writeJSON(jsonPath, results); err != nil {
			return err
		}
	}

	if cfg.WriteMD {
		if err := writeMarkdown(mdPath, results); err != nil {
			return err
		}
	}

	if !cfg.KeepOutputs {
		_ = os.RemoveAll(encodedDir)
	}

	fmt.Printf("\nWrote files:\n")
	fmt.Printf("  %s\n", scoredTSV)
	if cfg.WriteJSON {
		fmt.Printf("  %s\n", jsonPath)
	}
	if cfg.WriteMD {
		fmt.Printf("  %s\n", mdPath)
	}

	return nil
}

func parseConfig(args []string) (Config, bool, error) {
	cfg := Config{}
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}

	customInputs := hasAnyFlag(args, "--inputs", "-i")

	fs := flag.NewFlagSet("ffmpeg-benchmark", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	inputsCSV := fs.String("inputs", "video_720.mp4", "Comma-separated input files")
	inputsCSVShort := fs.String("i", "", "Alias of --inputs")
	resolution := fs.String("resolution", "720", "Resolution preset(s): 720,1080,4k,all (comma-separated)")
	resolutionShort := fs.String("r", "", "Alias of --resolution")
	codecsCSV := fs.String("codecs", "h264,h265,av1", "Comma-separated codecs: h264,h265,av1 (hevc alias supported)")
	codecsCSVShort := fs.String("c", "", "Alias of --codecs")
	durationSec := fs.Float64("duration-sec", 5, "Clip duration in seconds per test")
	durationSecShort := fs.Float64("d", 0, "Alias of --duration-sec")
	outDir := fs.String("outdir", "./bench_out", "Output directory")
	outDirShort := fs.String("o", "", "Alias of --outdir")
	preset := fs.String("preset", "medium", "Preset for h264/h265")
	presetShort := fs.String("p", "", "Alias of --preset")
	crfH264 := fs.Int("crf-h264", 23, "CRF for h264")
	crfH265 := fs.Int("crf-h265", 28, "CRF for h265")
	crfHEVC := fs.Int("crf-hevc", 28, "Alias of --crf-h265")
	crfAV1 := fs.Int("crf-av1", 32, "CRF/quality value for av1")
	maxJobs := fs.Int("max-jobs", 1, "Reserved for future parallel runs; currently only 1 is supported")
	maxJobsShort := fs.Int("j", 0, "Alias of --max-jobs")
	keepOutputs := fs.Bool("keep-outputs", false, "Keep encoded outputs")
	keepOutputsShort := fs.Bool("k", false, "Alias of --keep-outputs")
	writeJSON := fs.Bool("json", true, "Write results.json")
	writeMD := fs.Bool("markdown", true, "Write results.md")
	noJSON := fs.Bool("no-json", false, "Skip JSON output")
	noMD := fs.Bool("no-markdown", false, "Skip Markdown output")
	showVersion := fs.Bool("version", false, "Print version")
	showVersionShort := fs.Bool("v", false, "Alias of --version")
	showHelp := fs.Bool("help", false, "Show help")
	showHelpShort := fs.Bool("h", false, "Alias of --help")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cfg, false, errShowHelp
		}
		return cfg, false, err
	}

	if *showHelp || *showHelpShort {
		return cfg, false, errShowHelp
	}

	if *showVersion || *showVersionShort {
		return cfg, true, nil
	}

	inputsSpec := *inputsCSV
	if hasFlag(args, "-i") {
		inputsSpec = *inputsCSVShort
	}
	resolutionSpec := *resolution
	if hasFlag(args, "-r") {
		resolutionSpec = *resolutionShort
	}
	codecsSpec := *codecsCSV
	if hasFlag(args, "-c") {
		codecsSpec = *codecsCSVShort
	}
	durationVal := *durationSec
	if hasFlag(args, "-d") {
		durationVal = *durationSecShort
	}
	outDirVal := *outDir
	if hasFlag(args, "-o") {
		outDirVal = *outDirShort
	}
	presetVal := *preset
	if hasFlag(args, "-p") {
		presetVal = *presetShort
	}
	maxJobsVal := *maxJobs
	if hasFlag(args, "-j") {
		maxJobsVal = *maxJobsShort
	}
	keepOutputsVal := *keepOutputs || *keepOutputsShort

	var inputs []string
	var err error
	if customInputs {
		inputs = parseCSV(inputsSpec)
		if len(inputs) == 0 {
			return cfg, false, errors.New("no valid inputs provided")
		}
	} else {
		inputs, err = resolveInputsFromResolution(resolutionSpec)
		if err != nil {
			return cfg, false, err
		}
	}

	codecs, err := parseCodecs(codecsSpec)
	if err != nil {
		return cfg, false, err
	}

	if durationVal <= 0 {
		return cfg, false, errors.New("--duration-sec must be a positive number")
	}

	if maxJobsVal < 1 {
		return cfg, false, errors.New("--max-jobs must be >= 1")
	}

	if *noJSON {
		*writeJSON = false
	}
	if *noMD {
		*writeMD = false
	}

	cfg = Config{
		Inputs:       inputs,
		Resolution:   normalizeResolutionSpec(resolutionSpec, customInputs),
		Codecs:       codecs,
		DurationSec:  durationVal,
		OutDir:       outDirVal,
		Preset:       presetVal,
		CRFH264:      *crfH264,
		CRFH265:      *crfH265,
		CRFAV1:       *crfAV1,
		MaxJobs:      maxJobsVal,
		KeepOutputs:  keepOutputsVal,
		WriteJSON:    *writeJSON,
		WriteMD:      *writeMD,
		CustomInputs: customInputs,
	}

	if hasFlag(args, "--crf-hevc") && !hasFlag(args, "--crf-h265") {
		cfg.CRFH265 = *crfHEVC
	}

	return cfg, false, nil
}

func hasAnyFlag(args []string, names ...string) bool {
	for _, name := range names {
		if hasFlag(args, name) {
			return true
		}
	}
	return false
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}

func parseCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func parseCodecs(v string) ([]string, error) {
	parts := parseCSV(strings.ToLower(v))
	if len(parts) == 0 {
		return nil, errors.New("no valid codecs provided")
	}

	seen := map[string]bool{}
	codecs := make([]string, 0, len(parts))
	for _, c := range parts {
		canonical := ""
		switch c {
		case "h264", "av1":
			canonical = c
		case "h265", "hevc":
			canonical = "h265"
		default:
			return nil, fmt.Errorf("unsupported codec: %s (allowed: h264, h265, av1)", c)
		}
		if !seen[canonical] {
			seen[canonical] = true
			codecs = append(codecs, canonical)
		}
	}
	return codecs, nil
}

func resolveInputsFromResolution(spec string) ([]string, error) {
	tokens := parseCSV(strings.ToLower(spec))
	if len(tokens) == 0 {
		return nil, errors.New("no valid resolution provided")
	}

	added := map[string]bool{}
	inputs := make([]string, 0, 3)
	add := func(path string) {
		if !added[path] {
			added[path] = true
			inputs = append(inputs, path)
		}
	}

	for _, token := range tokens {
		switch token {
		case "720", "720p":
			add("video_720.mp4")
		case "1080", "1080p", "fhd":
			add("video_1080.mp4")
		case "4k", "2160", "2160p", "uhd":
			add("video.mp4")
		case "all":
			add("video_720.mp4")
			add("video_1080.mp4")
			add("video.mp4")
		default:
			return nil, fmt.Errorf("unsupported resolution: %s (allowed: 720, 1080, 4k, all)", token)
		}
	}

	if len(inputs) == 0 {
		return nil, errors.New("no valid resolution provided")
	}

	return inputs, nil
}

func normalizeResolutionSpec(spec string, customInputs bool) string {
	if customInputs {
		return "custom"
	}
	tokens := parseCSV(strings.ToLower(spec))
	if len(tokens) == 0 {
		return "720"
	}
	return strings.Join(tokens, ",")
}

func collectSystemInfo(ffmpegPath string) SystemInfo {
	osName := firstLineOutput("uname", "-s")
	if osName == "" {
		osName = runtime.GOOS
	}
	osRelease := firstLineOutput("uname", "-r")
	if osRelease == "" {
		osRelease = "unknown"
	}

	cpuModel := ""
	if runtime.GOOS == "darwin" {
		cpuModel = firstLineOutput("sysctl", "-n", "machdep.cpu.brand_string")
	}
	if cpuModel == "" && runtime.GOOS == "linux" {
		if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if strings.Contains(line, "model name") && strings.Contains(line, ":") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						cpuModel = strings.TrimSpace(parts[1])
						break
					}
				}
			}
		}
	}
	if cpuModel == "" {
		cpuModel = "unknown"
	}

	ffmpegVersion := firstLineOutput(ffmpegPath, "-version")
	if ffmpegVersion == "" {
		ffmpegVersion = "unknown"
	}

	return SystemInfo{
		OS:           osName,
		OSRelease:    osRelease,
		CPUArch:      runtime.GOARCH,
		CPUModel:     cpuModel,
		LogicalCores: runtime.NumCPU(),
		FFmpeg:       ffmpegVersion,
	}
}

func detectEncoders(ffmpegPath string) (EncoderAvailability, error) {
	cmd := exec.Command(ffmpegPath, "-hide_banner", "-encoders")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return EncoderAvailability{}, fmt.Errorf("failed to list ffmpeg encoders: %w", err)
	}

	text := string(out)
	avail := EncoderAvailability{}
	if hasToken(text, "libx264") {
		avail.H264 = "libx264"
	}
	if hasToken(text, "libx265") {
		avail.H265 = "libx265"
	}
	if hasToken(text, "libsvtav1") {
		avail.AV1 = "libsvtav1"
	} else if hasToken(text, "librav1e") {
		avail.AV1 = "librav1e"
	}

	return avail, nil
}

func hasToken(haystack, token string) bool {
	re := regexp.MustCompile(`(^|\s)` + regexp.QuoteMeta(token) + `(\s|$)`)
	return re.MatchString(haystack)
}

func chooseEncoder(codec string, avail EncoderAvailability) string {
	switch codec {
	case "h264":
		return avail.H264
	case "h265":
		return avail.H265
	case "av1":
		return avail.AV1
	default:
		return ""
	}
}

func runEncode(ffmpegPath, input, output, codec, encoder string, cfg Config, durationSec float64, loopInput bool) (string, time.Duration, []string, error) {
	codecArgs := make([]string, 0, 8)
	switch codec {
	case "h264":
		codecArgs = append(codecArgs, "-c:v", encoder, "-preset", cfg.Preset, "-crf", strconv.Itoa(cfg.CRFH264))
	case "h265":
		codecArgs = append(codecArgs, "-c:v", encoder, "-preset", cfg.Preset, "-crf", strconv.Itoa(cfg.CRFH265))
	case "av1":
		if encoder == "libsvtav1" {
			codecArgs = append(codecArgs, "-c:v", encoder, "-preset", "6", "-crf", strconv.Itoa(cfg.CRFAV1))
		} else {
			codecArgs = append(codecArgs, "-c:v", encoder, "-speed", "6", "-qp", strconv.Itoa(cfg.CRFAV1))
		}
	}

	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	if loopInput {
		args = append(args, "-stream_loop", "-1")
	}
	args = append(args,
		"-i", input,
		"-t", formatFloat(durationSec, 6),
	)
	args = append(args, codecArgs...)
	args = append(args, "-an", output)

	cmd := exec.Command(ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = nil

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	return stderr.String(), elapsed, args, err
}

func syntheticInputSpec(input string) (label string, width int, height int, ok bool) {
	switch filepath.Base(input) {
	case "video_720.mp4":
		return "720p", 1280, 720, true
	case "video_1080.mp4":
		return "1080p", 1920, 1080, true
	case "video.mp4":
		return "4K", 3840, 2160, true
	default:
		return "", 0, 0, false
	}
}

func generateSyntheticInput(ffmpegPath, output string, width, height int, durationSec float64) error {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=%dx%d:rate=30", width, height),
		"-t", formatFloat(durationSec, 6),
		"-pix_fmt", "yuv420p",
		"-c:v", "mpeg4", "-q:v", "5",
		output,
	}
	cmd := exec.Command(ffmpegPath, args...)
	return cmd.Run()
}

func resolutionInfo(ffprobePath, input string) (string, float64) {
	switch filepath.Base(input) {
	case "video.mp4":
		return "4K", 0.50
	case "video_1080.mp4":
		return "1080p", 0.30
	case "video_720.mp4":
		return "720p", 0.20
	}

	w, h := probeDimensions(ffprobePath, input)
	if w == 0 || h == 0 {
		return "unknown", 0.20
	}
	if h >= 2000 {
		return ">=4K", 0.50
	}
	if h >= 1000 {
		return "1080p", 0.30
	}
	if h >= 700 {
		return "720p", 0.20
	}
	return "unknown", 0.20
}

func probeDimensions(ffprobePath, input string) (int, int) {
	cmd := exec.Command(ffprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		input,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "x")
	if len(parts) != 2 {
		return 0, 0
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil {
		return 0, 0
	}
	return w, h
}

func probeDuration(ffprobePath, input string) float64 {
	cmd := exec.Command(ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=nokey=1:noprint_wrappers=1",
		input,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

func codecWeight(codec string) float64 {
	switch codec {
	case "h264":
		return 0.20
	case "h265":
		return 0.35
	case "av1":
		return 0.45
	default:
		return 0
	}
}

func scoreCases(cases []CaseResult) {
	totalRaw := 0.0
	for _, c := range cases {
		totalRaw += c.RawWeight
	}
	if totalRaw <= 0 {
		totalRaw = 1
	}

	for i := range cases {
		weightNorm := cases[i].RawWeight / totalRaw
		normalizedSpeed := cases[i].SpeedX / 5.0
		if normalizedSpeed > 1.0 {
			normalizedSpeed = 1.0
		}
		if cases[i].Status != "OK" {
			normalizedSpeed = 0
		}
		cases[i].WeightNorm = weightNorm
		cases[i].CaseScore = weightNorm * normalizedSpeed
	}
}

func summarize(cases []CaseResult) Summary {
	summary := Summary{}
	sumScore := 0.0
	sumLog := 0.0
	nOKSpeed := 0

	for _, c := range cases {
		sumScore += c.CaseScore
		switch c.Status {
		case "OK":
			summary.SuccessfulCases++
			if c.SpeedX > 0 {
				sumLog += math.Log(c.SpeedX)
				nOKSpeed++
			}
		case "FAILED":
			summary.FailedCases++
		case "UNSUPPORTED":
			summary.UnsupportedCases++
		}
	}

	if nOKSpeed > 0 {
		summary.GeomeanSpeedX = math.Exp(sumLog / float64(nOKSpeed))
	}
	summary.TotalScore1000 = int(math.Round(sumScore * 1000))

	return summary
}

func writeScoredTSV(path string, cases []CaseResult) error {
	var b strings.Builder
	b.WriteString("input\tcodec\tencoder\tres_label\tres_weight\tcodec_weight\traw_weight\tduration_sec\telapsed_sec\tspeed_x\tstatus\toutput_file\tstderr_tail\tweight_norm\tcase_score\n")
	for _, c := range cases {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%.6f\t%.6f\t%.12f\t%.6f\t%.6f\t%.6f\t%s\t%s\t%s\t%.12f\t%.12f\n",
			c.Input,
			c.Codec,
			c.Encoder,
			c.ResolutionLabel,
			c.ResolutionWeight,
			c.CodecWeight,
			c.RawWeight,
			c.DurationSec,
			c.ElapsedSec,
			c.SpeedX,
			c.Status,
			c.OutputFile,
			c.StderrTail,
			c.WeightNorm,
			c.CaseScore,
		)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeJSON(path string, results Results) error {
	payload, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(path, payload, 0o644)
}

func writeMarkdown(path string, results Results) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Video Encode Benchmark Results\n\n")
	fmt.Fprintf(&b, "- Run date (UTC): `%s`\n", results.RunDateUTC)
	fmt.Fprintf(&b, "- Script version: `%s`\n", results.ScriptVersion)
	fmt.Fprintf(&b, "- Clip duration per test: `%ss`\n", formatDurationValue(results.RunConfig.ClipDurationSec))
	fmt.Fprintf(&b, "- Total score: **%d / 1000**\n", results.Summary.TotalScore1000)
	fmt.Fprintf(&b, "- Geomean speed (OK cases): **%sx**\n\n", formatFloat(results.Summary.GeomeanSpeedX, 4))

	b.WriteString("## System\n\n")
	b.WriteString("| Key | Value |\n")
	b.WriteString("|---|---|\n")
	fmt.Fprintf(&b, "| OS | `%s %s` |\n", results.System.OS, results.System.OSRelease)
	fmt.Fprintf(&b, "| CPU | `%s` |\n", results.System.CPUModel)
	fmt.Fprintf(&b, "| Logical cores | `%d` |\n", results.System.LogicalCores)
	fmt.Fprintf(&b, "| ffmpeg | `%s` |\n\n", results.System.FFmpeg)

	b.WriteString("## Cases\n\n")
	b.WriteString("| Input | Codec | Encoder | Status | Duration (s) | Elapsed (s) | Speed (x) | Weight | Case Score |\n")
	b.WriteString("|---|---|---|---|---:|---:|---:|---:|---:|\n")
	for _, c := range results.Cases {
		fmt.Fprintf(&b,
			"| `%s` | `%s` | `%s` | `%s` | %.3f | %.3f | %.3f | %.4f | %.4f |\n",
			filepath.Base(c.Input), c.Codec, fallback(c.Encoder, "n/a"), c.Status,
			c.DurationSec, c.ElapsedSec, c.SpeedX, c.WeightNorm, c.CaseScore,
		)
	}

	ranked := rankedCases(results.Cases)
	b.WriteString("\n## Ranking (By Speed)\n\n")
	b.WriteString("| Rank | Codec | Status | Speed (x) | Elapsed (s) | Case Score |\n")
	b.WriteString("|---:|---|---|---:|---:|---:|\n")
	for i, c := range ranked {
		fmt.Fprintf(&b, "| %d | `%s` | `%s` | %.3f | %.3f | %.4f |\n", i+1, c.Codec, c.Status, c.SpeedX, c.ElapsedSec, c.CaseScore)
	}

	b.WriteString("\n## Summary\n\n")
	fmt.Fprintf(&b, "- Successful cases: `%d`\n", results.Summary.SuccessfulCases)
	fmt.Fprintf(&b, "- Failed cases: `%d`\n", results.Summary.FailedCases)
	fmt.Fprintf(&b, "- Unsupported cases: `%d`\n", results.Summary.UnsupportedCases)

	if results.Summary.FailedCases > 0 || results.Summary.UnsupportedCases > 0 {
		b.WriteString("\n## Non-OK Cases\n\n")
		for _, c := range results.Cases {
			if c.Status == "OK" {
				continue
			}
			if c.StderrTail == "" {
				fmt.Fprintf(&b, "- `%s + %s`: `%s`\n", filepath.Base(c.Input), c.Codec, c.Status)
			} else {
				fmt.Fprintf(&b, "- `%s + %s`: `%s` (`%s`)\n", filepath.Base(c.Input), c.Codec, c.Status, c.StderrTail)
			}
		}
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func printCaseTable(cases []CaseResult) {
	fmt.Printf("\nCase results:\n")
	fmt.Printf("%-16s %-6s %-11s %-9s %-9s %-8s %-8s\n", "Input", "Codec", "Status", "Speed", "Elapsed", "Weight", "Score")
	fmt.Printf("%s\n", "---------------------------------------------------------------------------")
	for _, c := range cases {
		fmt.Printf("%-16s %-6s %-11s %-9s %-9s %-8s %-8s\n",
			filepath.Base(c.Input),
			c.Codec,
			c.Status,
			formatSpeed(c.SpeedX),
			formatSeconds(c.ElapsedSec),
			formatFloat(c.WeightNorm, 3),
			formatFloat(c.CaseScore, 3),
		)
	}
}

func printRanking(cases []CaseResult) {
	ranked := rankedCases(cases)
	fmt.Printf("\nCodec ranking (by speed):\n")
	fmt.Printf("%-4s %-6s %-11s %-9s %-9s %-9s\n", "Rank", "Codec", "Status", "Speed", "Elapsed", "Score")
	fmt.Printf("%s\n", "--------------------------------------------------------------")
	for i, c := range ranked {
		fmt.Printf("%-4d %-6s %-11s %-9s %-9s %-9s\n",
			i+1,
			c.Codec,
			c.Status,
			formatSpeed(c.SpeedX),
			formatSeconds(c.ElapsedSec),
			formatFloat(c.CaseScore, 3),
		)
	}
}

func printSummary(summary Summary) {
	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Successful cases:  %d\n", summary.SuccessfulCases)
	fmt.Printf("  Failed cases:      %d\n", summary.FailedCases)
	fmt.Printf("  Unsupported cases: %d\n", summary.UnsupportedCases)
	fmt.Printf("  Geomean speed:     %sx\n", formatFloat(summary.GeomeanSpeedX, 4))
	fmt.Printf("  Total score:       %d / 1000\n", summary.TotalScore1000)
}

func rankedCases(cases []CaseResult) []CaseResult {
	cp := make([]CaseResult, len(cases))
	copy(cp, cases)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i].SpeedX == cp[j].SpeedX {
			return cp[i].Codec < cp[j].Codec
		}
		return cp[i].SpeedX > cp[j].SpeedX
	})
	return cp
}

func compactLog(stderr string) string {
	if stderr == "" {
		return ""
	}
	norm := strings.ReplaceAll(stderr, "\r\n", "\n")
	norm = strings.ReplaceAll(norm, "\t", " ")
	lines := strings.Split(norm, "\n")
	clean := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			clean = append(clean, line)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	const maxLines = 30
	if len(clean) > maxLines {
		clean = clean[len(clean)-maxLines:]
	}
	return strings.Join(clean, "\\n")
}

func appendMsg(msg, extra string) string {
	if msg == "" {
		return extra
	}
	return msg + "\\n" + extra
}

func writeCaseLog(path string, args []string, elapsedSec float64, stderr string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "command: ffmpeg %s\n", strings.Join(args, " "))
	fmt.Fprintf(&b, "elapsed_sec: %.6f\n", elapsedSec)
	b.WriteString("\nstderr:\n")
	b.WriteString(stderr)
	if !strings.HasSuffix(stderr, "\n") {
		b.WriteString("\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func firstLineOutput(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "\n")
	return strings.TrimSpace(parts[0])
}

func formatFloat(v float64, decimals int) string {
	return strconv.FormatFloat(v, 'f', decimals, 64)
}

func formatDurationValue(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func formatSeconds(v float64) string {
	return formatFloat(v, 2) + "s"
}

func formatSpeed(v float64) string {
	return formatFloat(v, 2) + "x"
}

func fallback(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func printUsage() {
	fmt.Println("Usage: ffmpeg-benchmark [run] [options]")
	fmt.Println()
	fmt.Println("Default profile:")
	fmt.Println("  Resolution: 720 (video_720.mp4; auto-generated if missing)")
	fmt.Println("  Clip duration: 5 seconds per test")
	fmt.Println("  Codecs: h264,h265,av1")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --inputs, -i CSV     Comma-separated input files")
	fmt.Println("  --resolution, -r CSV Resolution preset(s): 720,1080,4k,all (default: 720)")
	fmt.Println("  --codecs, -c CSV     Comma-separated codecs: h264,h265,av1 (hevc alias supported)")
	fmt.Println("  --duration-sec, -d N Clip duration in seconds per test (default: 5)")
	fmt.Println("  --outdir, -o DIR     Output directory (default: ./bench_out)")
	fmt.Println("  --preset, -p NAME    Preset for h264/h265 (default: medium)")
	fmt.Println("  --crf-h264 N         CRF for h264 (default: 23)")
	fmt.Println("  --crf-h265 N         CRF for h265 (default: 28)")
	fmt.Println("  --crf-hevc N         Alias of --crf-h265")
	fmt.Println("  --crf-av1 N          CRF/quality value for av1 (default: 32)")
	fmt.Println("  --max-jobs, -j N     Reserved for future parallel runs; currently only 1 is supported")
	fmt.Println("  --keep-outputs, -k   Keep encoded output videos")
	fmt.Println("  --json               Write results.json (enabled by default)")
	fmt.Println("  --markdown           Write results.md (enabled by default)")
	fmt.Println("  --no-json            Skip JSON output")
	fmt.Println("  --no-markdown        Skip Markdown output")
	fmt.Println("  --version, -v        Print version")
	fmt.Println("  --help, -h           Show help")
	fmt.Println()
	fmt.Println("Notes:")
	fmt.Println("  - --inputs has priority over --resolution")
	fmt.Println("  - Missing preset files are auto-generated for non-custom input mode")
}
