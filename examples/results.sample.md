# Sample Output (Excerpt)

```text
Case results:
Input            Res     Codec  Status      Speed     Elapsed   Score
--------------------------------------------------------------------------------
video_720.mp4    720p    h264   OK          7.81x     0.64s     0.200
video_720.mp4    720p    h265   OK          3.14x     1.59s     0.220
video_720.mp4    720p    av1    OK          3.21x     1.56s     0.289

Summary:
  Successful cases:  3
  Failed cases:      0
  Unsupported cases: 0
  Geomean speed:     4.2830x
  Score:             709 / 1000

Resolution scores:
Resolution OK   Fail   Unsupported Geomean   Score
---------------------------------------------------------------------
720p       3    0      0           4.28x     709/1000
```

Summary fields in `results.json`:
- `successful_cases`
- `failed_cases`
- `unsupported_cases`
- `geomean_speed_x`
- `overall_score_1000`
- `overall_score_basis`
- `resolution_scores[]`
- `total_score_1000` (legacy alias of `overall_score_1000`)
