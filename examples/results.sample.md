# Sample Output (Excerpt)

```text
Case results:
Input            Codec  Status      Speed     Elapsed   Weight   Score
---------------------------------------------------------------------------
video_720.mp4    h264   OK          7.81x     0.64s     0.200    0.200
video_720.mp4    h265   OK          3.14x     1.59s     0.350    0.220
video_720.mp4    av1    OK          3.21x     1.56s     0.450    0.289

Codec ranking (by speed):
Rank Codec  Status      Speed     Elapsed   Score
--------------------------------------------------------------
1    h264   OK          7.81x     0.64s     0.200
2    av1    OK          3.21x     1.56s     0.289
3    h265   OK          3.14x     1.59s     0.220
```

Summary fields in `results.json`:
- `successful_cases`
- `failed_cases`
- `unsupported_cases`
- `geomean_speed_x`
- `total_score_1000`
