package main

import (
	"math"
	"testing"
)

func TestClassifyResolutionByHeight(t *testing.T) {
	tests := []struct {
		height    int
		wantLabel string
	}{
		{2160, "4K"},
		{2000, "4K"},
		{1080, "1080p"},
		{720, "720p"},
		{480, "unknown"},
	}

	for _, tc := range tests {
		gotLabel := classifyResolutionByHeight(tc.height)
		if gotLabel != tc.wantLabel {
			t.Fatalf("height=%d label: got %q want %q", tc.height, gotLabel, tc.wantLabel)
		}
	}
}

func TestSummarizeBenchmarkScorePerResolution(t *testing.T) {
	cases := []CaseResult{
		{ResolutionLabel: "720p", SpeedX: 5.0, Status: "OK"},
		{ResolutionLabel: "720p", SpeedX: 2.5, Status: "OK"},
		{ResolutionLabel: "720p", SpeedX: 0.0, Status: "FAILED"},
		{ResolutionLabel: "1080p", SpeedX: 5.0, Status: "OK"},
		{ResolutionLabel: "1080p", SpeedX: 1.0, Status: "OK"},
		{ResolutionLabel: "1080p", SpeedX: 0.0, Status: "UNSUPPORTED"},
	}

	summary := summarize(cases)

	if !almostEqual(summary.GeomeanSpeedX, 2.8117066259517456, 1e-12) {
		t.Fatalf("overall geomean speed: got %.12f", summary.GeomeanSpeedX)
	}
	if !almostEqual(summary.BenchmarkScore, 281.17066259517455, 1e-9) {
		t.Fatalf("overall benchmark score: got %.12f", summary.BenchmarkScore)
	}

	if len(summary.ResolutionScores) != 2 {
		t.Fatalf("resolution count: got %d want 2", len(summary.ResolutionScores))
	}

	first := summary.ResolutionScores[0]
	second := summary.ResolutionScores[1]

	if first.ResolutionLabel != "720p" {
		t.Fatalf("first resolution: got %q want 720p", first.ResolutionLabel)
	}
	if !almostEqual(first.GeomeanSpeedX, 3.5355339059327378, 1e-12) {
		t.Fatalf("720 geomean: got %.12f", first.GeomeanSpeedX)
	}
	if !almostEqual(first.BenchmarkScore, 353.5533905932738, 1e-9) {
		t.Fatalf("720 score: got %.12f", first.BenchmarkScore)
	}

	if second.ResolutionLabel != "1080p" {
		t.Fatalf("second resolution: got %q want 1080p", second.ResolutionLabel)
	}
	if !almostEqual(second.GeomeanSpeedX, 2.23606797749979, 1e-12) {
		t.Fatalf("1080 geomean: got %.12f", second.GeomeanSpeedX)
	}
	if !almostEqual(second.BenchmarkScore, 223.606797749979, 1e-9) {
		t.Fatalf("1080 score: got %.12f", second.BenchmarkScore)
	}
}

func TestSummarizeSingleResolution(t *testing.T) {
	cases := []CaseResult{
		{ResolutionLabel: "720p", SpeedX: 5.0, Status: "OK"},
		{ResolutionLabel: "720p", SpeedX: 5.0, Status: "OK"},
		{ResolutionLabel: "720p", SpeedX: 5.0, Status: "OK"},
	}

	summary := summarize(cases)
	if !almostEqual(summary.GeomeanSpeedX, 5.0, 1e-12) {
		t.Fatalf("geomean speed: got %.12f want 5.0", summary.GeomeanSpeedX)
	}
	if !almostEqual(summary.BenchmarkScore, 500.0, 1e-12) {
		t.Fatalf("benchmark score: got %.12f want 500.0", summary.BenchmarkScore)
	}
	if len(summary.ResolutionScores) != 1 {
		t.Fatalf("resolution count: got %d want 1", len(summary.ResolutionScores))
	}
}

func almostEqual(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}
