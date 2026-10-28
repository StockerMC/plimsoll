settle_s: seconds from the last event, or the start, until the target stays within 10% of its final value.

| scenario | recommender | final_target_mib | min_target_mib | max_target_mib | oom_kills | settle_s |
| --- | --- | --- | --- | --- | --- | --- |
| steady | ours | 454 | 250 | 454 | 0 | 195 |
| steady | ensemble | 454 | 250 | 454 | 0 | 195 |
| steady | upstream | 454 | 309 | 454 | 0 | 120 |
| burst | ours | 600 | 250 | 600 | 1 | 435 |
| burst | ensemble | 600 | 250 | 600 | 1 | 495 |
| burst | upstream | 600 | 309 | 600 | 1 | 405 |
| restart | ours | 454 | 250 | 454 | 0 | 0 |
| restart | ensemble | 454 | 250 | 454 | 0 | 75 |
| restart | upstream | 454 | 250 | 454 | 0 | 0 |
| missing-metrics | ours | 454 | 250 | 454 | 0 | 0 |
| missing-metrics | ensemble | 454 | 250 | 454 | 0 | 0 |
| missing-metrics | upstream | 454 | 309 | 454 | 0 | 0 |

Every ensemble row is its cold-start rule (largest peak seen, with margin): at 1-minute windows no scenario outlasts the 12-window cold start, so the selector never set a target here.

restart: ours restored its checkpoint and held 476450464 through the restart at 240 s; the ensemble keeps no checkpoint and sat at the 250 Mi floor from 240 to 300 s; upstream's status did not move. The 250 MiB minimum on every side is the warm-up before 135 s.
