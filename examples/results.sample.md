# Sample Output (Excerpt)

```text
Case results:
Input            Res     Codec  Status      Speed     Elapsed   RT%
----------------------------------------------------------------------------------
video_720.mp4    720p    h264   OK          7.81x     0.64s     781.00
video_720.mp4    720p    h265   OK          3.14x     1.59s     314.00
video_720.mp4    720p    av1    OK          3.21x     1.56s     321.00

Summary:
  Geomean speed:     4.2830x
  Benchmark score:   428.30 (100 = 1.0x real-time)

Resolution scores:
Resolution Geomean   Score
---------------------------------------
720p       4.28x     428.30
```

Summary fields in `results.json`:
- `successful_cases`
- `failed_cases`
- `unsupported_cases`
- `geomean_speed_x`
- `benchmark_score`
- `resolution_scores[]`
