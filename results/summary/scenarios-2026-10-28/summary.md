settle_s: seconds from the last event, or the start, until the target stays within 10% of its final value.

| scenario | recommender | final_target_mib | min_target_mib | max_target_mib | oom_kills | settle_s |
| --- | --- | --- | --- | --- | --- | --- |
| steady | ours | 454 | 250 | 454 | 0 | 195 |
| steady | ensemble | 454 | 250 | 454 | 0 | 195 |
| steady | upstream | 454 | 392 | 454 | 0 | 120 |
| burst | ours | 600 | 250 | 600 | 1 | 435 |
| burst | ensemble | 600 | 250 | 600 | 1 | 495 |
| burst | upstream | 600 | 309 | 600 | 1 | 420 |
| restart | ours | 454 | 250 | 454 | 0 | 0 |
| restart | upstream | 454 | 392 | 454 | 0 | 0 |

Every ensemble row is its cold-start rule (largest peak seen, with margin): at 1-minute windows no scenario outlasts the 12-window cold start, so the selector never set a target here.
