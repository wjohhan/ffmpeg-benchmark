# Sample Output (Excerpt)

```text
Case results:
Input            Codec  Status      Speed     Elapsed   Weight   Score
---------------------------------------------------------------------------
video_720.mp4    h264   OK          0.58x     8.63s     0.200    0.023
video_720.mp4    h265   OK          0.28x     17.78s    0.350    0.020
video_720.mp4    av1    OK          0.23x     21.64s    0.450    0.021

Codec ranking (by speed):
Rank Codec  Status      Speed     Elapsed   Score
--------------------------------------------------------------
1    h264   OK          0.58x     8.63s     0.023
2    h265   OK          0.28x     17.78s    0.020
3    av1    OK          0.23x     21.64s    0.021
```

Summary fields in `results.json`:
- `successful_cases`
- `failed_cases`
- `unsupported_cases`
- `geomean_speed_x`
- `total_score_1000`
