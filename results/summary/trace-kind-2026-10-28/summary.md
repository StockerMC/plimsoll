One row per curve and recommender, VPAs in Off mode. scored: 15 s samples with both a target and a `kubectl top` usage; exceedance: scored samples with usage above the target; slack: mean (target - usage) / target over the scored samples. `kubectl top` carries no timestamp and lags the curve by 15 to 60 s, so a target can already include the usage it is scored against: this is a concurrent comparison, not a test of prediction ahead of time. The pods have no memory limit, so oom_kills only shows the curve ran uninterrupted. settle_s: seconds from the start until the target stays within 10% of its final value.

| curve | recommender | scored | exceedance | mean_slack | oom_kills | settle_s |
| --- | --- | --- | --- | --- | --- | --- |
| trace-303024365163 | ours | 193 | 6 | 0.5921 | 0 | 735 |
| trace-303024365163 | ensemble | 193 | 4 | 0.6039 | 0 | 735 |
| trace-303024365163 | upstream | 193 | 4 | 0.6042 | 0 | 675 |
| trace-359 | ours | 193 | 2 | 0.2570 | 0 | 915 |
| trace-359 | ensemble | 193 | 6 | 0.2206 | 0 | 1754 |
| trace-359 | upstream | 193 | 1 | 0.2067 | 0 | 1679 |
| trace-91076418940 | ours | 193 | 3 | 0.4867 | 0 | 615 |
| trace-91076418940 | ensemble | 193 | 4 | 0.4711 | 0 | 1755 |
| trace-91076418940 | upstream | 193 | 0 | 0.4320 | 0 | 480 |

Run at 60x compression (one 300 s trace window per 5 s, two days in 48 min) on the curves in deploy/curves/, all three namespaces at once. Every side starts watching before the pods exist and sees the same 120 s pre-roll holding each curve's first row. The recommenders sample at 1 min, so they see a time-compressed, coarsely sampled version of each curve, not the signal the replay scored. ensemble is ensemble-held from configs/default.json (--ensemble-overrun-weight 100 --ensemble-limit-change-weight 0 --ensemble-fall-fraction 0.1 --ensemble-fall-windows 24, its 288 five-minute windows at this compression) with --ensemble-time-scale 60 --ensemble-hour-windows 1. The curves are scaled so their p95 is 256 MiB, just above the 250 MiB floor on every side, so where a curve sits near its p95 (trace-359: median 238, peak 342 MiB) the targets sit at or near the floor for much of the run and part of the slack is the floor's.
