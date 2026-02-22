package main

import (
	"math"
	"testing"
)

func TestClassifyResolutionByHeight(t *testing.T) {
	tests := []struct {
		height     int
		wantLabel  string
		wantWeight float64
	}{
		{2160, "4K", 0.50},
		{2000, "4K", 0.50},
		{1080, "1080p", 0.30},
		{720, "720p", 0.20},
		{480, "unknown", 0.20},
	}

	for _, tc := range tests {
		gotLabel, gotWeight := classifyResolutionByHeight(tc.height)
		if gotLabel != tc.wantLabel {
			t.Fatalf("height=%d label: got %q want %q", tc.height, gotLabel, tc.wantLabel)
		}
		if !almostEqual(gotWeight, tc.wantWeight, 1e-9) {
			t.Fatalf("height=%d weight: got %.6f want %.6f", tc.height, gotWeight, tc.wantWeight)
		}
	}
}

func TestScoreAndSummaryPerResolution(t *testing.T) {
	cases := []CaseResult{
		{ResolutionLabel: "720p", RawWeight: 0.04, SpeedX: 5.0, Status: "OK"},
		{ResolutionLabel: "720p", RawWeight: 0.07, SpeedX: 2.5, Status: "OK"},
		{ResolutionLabel: "720p", RawWeight: 0.09, SpeedX: 0.0, Status: "FAILED"},
		{ResolutionLabel: "1080p", RawWeight: 0.06, SpeedX: 5.0, Status: "OK"},
		{ResolutionLabel: "1080p", RawWeight: 0.105, SpeedX: 1.0, Status: "OK"},
		{ResolutionLabel: "1080p", RawWeight: 0.135, SpeedX: 0.0, Status: "UNSUPPORTED"},
	}

	scoreCases(cases)
	summary := summarize(cases)

	if !almostEqual(cases[0].WeightNorm, 0.20, 1e-9) {
		t.Fatalf("unexpected 720 h264 weight norm: got %.6f want %.6f", cases[0].WeightNorm, 0.20)
	}
	if !almostEqual(cases[3].WeightNorm, 0.20, 1e-9) {
		t.Fatalf("unexpected 1080 h264 weight norm: got %.6f want %.6f", cases[3].WeightNorm, 0.20)
	}

	if len(summary.ResolutionScores) != 2 {
		t.Fatalf("resolution score count: got %d want 2", len(summary.ResolutionScores))
	}
	if summary.ResolutionScores[0].ResolutionLabel != "720p" || summary.ResolutionScores[0].Score1000 != 375 {
		t.Fatalf("unexpected first resolution summary: %+v", summary.ResolutionScores[0])
	}
	if summary.ResolutionScores[1].ResolutionLabel != "1080p" || summary.ResolutionScores[1].Score1000 != 270 {
		t.Fatalf("unexpected second resolution summary: %+v", summary.ResolutionScores[1])
	}

	if summary.OverallScore1000 != 323 {
		t.Fatalf("overall score: got %d want 323", summary.OverallScore1000)
	}
	if summary.TotalScore1000 != summary.OverallScore1000 {
		t.Fatalf("legacy total score should match overall: total=%d overall=%d", summary.TotalScore1000, summary.OverallScore1000)
	}
	if summary.OverallScoreBasis != "mean_of_resolution_scores" {
		t.Fatalf("overall score basis: got %q want %q", summary.OverallScoreBasis, "mean_of_resolution_scores")
	}
}

func TestSummarySingleResolutionBasis(t *testing.T) {
	cases := []CaseResult{
		{ResolutionLabel: "720p", RawWeight: 0.04, SpeedX: 5.0, Status: "OK"},
		{ResolutionLabel: "720p", RawWeight: 0.07, SpeedX: 5.0, Status: "OK"},
		{ResolutionLabel: "720p", RawWeight: 0.09, SpeedX: 5.0, Status: "OK"},
	}

	scoreCases(cases)
	summary := summarize(cases)

	if summary.OverallScoreBasis != "single_resolution_score" {
		t.Fatalf("overall score basis: got %q want %q", summary.OverallScoreBasis, "single_resolution_score")
	}
	if summary.OverallScore1000 != 1000 {
		t.Fatalf("overall score: got %d want 1000", summary.OverallScore1000)
	}
}

func almostEqual(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}
