# results

`results/raw/` is not committed: per-collection series and logs. `results/summary/` is: one directory per replay run, the sweep tables, the comparisons, and the kind scenario runs.

`restart-<run>/` holds the restart scenario's targets and checkpoint dumps; `savings-<run>/` the actuated savings run, with its definitions in `summary.md`.

## Replay runs

`RUN=cella-<unique-id> scripts/replay-all.sh sweep` replays every entry of `configs/sweep-cella.json` on the tune split and writes `sweep-$RUN.csv`, one row per run. Run `bin/replay --sweep configs/sweep-beyond.json --split tune --out results/summary/sweep-beyond-<unique-id>.csv` for the fall-hysteresis grid and the corresponding command with `sweep-ratio.json` for the learned-margin grid. The window reference (p98, 48 h half-life, 12-window peak and max, 15%/10% margin) is never swept. A row is eligible when its exceedance windows and limit changes per collection-day are each within 10% of the reference's; the chosen row per recommender is the eligible one with the lowest mean slack. The chosen values are copied into `configs/default.json` by hand and committed before the report split is run.

`RUN=cella-<unique-id> scripts/replay-all.sh report` replays the report split with every recommender under both actuation rules and writes `$RUN-<recommender>-<actuator>/summary.json` and `collections.csv` (one row per collection: the summary's inputs). Use the same `RUN` with `scripts/replay-all.sh compare`; it reads those exact report paths and writes `$RUN-comparisons/compare-<date>/<candidate>-vs-<baseline>.{json,md}` for the pre-registered pairs. An existing report prefix or comparison directory is refused, so reruns need a new `RUN`.

Recommenders: `percentile` (the upstream VPA port at upstream's defaults), `percentile-tuned` (the sweep's pick), `window` (the frozen reference), `ml` (arg-min models only, the configuration of the Autopilot paper), `ensemble` (the same selector with the window reference as one more arm), `ensemble-held` (the ensemble whose recommendation follows a fall in the selector's hourly max only after it has stayed at least `fall_fraction` lower for `fall_windows` windows in a row, rises followed at once; its weights and the two thresholds are the `ensemble-held` block of `configs/default.json`, chosen by `sweep-beyond-margin-20261029.csv`), `ensemble-ratio` (the held ensemble with one arg-min arm per half-life whose margin is learned instead of the grid of fixed margins: each arm's limit is its raw limit times the largest ratio of a window's peak to the raw limit the arm held before that window over its last `ratio_windows` windows, at least `ratio_floor`; the window arm keeps its step margin; parameters in the `ensemble-ratio` block of `configs/default.json`, chosen by `sweep-ratio-margin-20261029.csv`), `assigned` (the trace's own `assigned_memory`, the sanity check for the metric), `naive` (the largest peak of the last 48 h plus 15%, the tracking control).

Actuators: `every` sets the limit to the target whenever it changes. `updater` is a simplified form of upstream VPA's updater: change when the request is outside the recommendation's bounds, or differs from the target by 10% or more and the last change is at least 12 h old (upstream checks pod age instead); the request starts at the trace's `assigned_memory` of the collection's first window. `window`, `ml`, `ensemble`, `ensemble-held`, `ensemble-ratio`, `assigned` and `naive` report bounds equal to their target, for which the bounds test would fire at every drift, so for them only the 10%/12 h rule applies.

The numbers below are `cella-2026-10-30-*`: binary 64bfc21, the fixed-margin grid {5%, 10%, 15%, 25%} with no zero-margin arm, parameters from `sweep-*-margin-20261029.csv` frozen in `configs/default.json`. The grid change was made after earlier report runs had been seen. `cella-2026-10-28-*` and `compare-2026-10-28/` are the earlier runs with zero-margin arms, kept as history; their `percentile`, `percentile-tuned`, `window`, `assigned` and `naive` rows are byte-identical to the new ones.

## Cell e

`celle-2026-10-30-*` replay all 312 sampled collections of cell e with `configs/default.json` unchanged, under the protocol in `protocol-celle-2026-10-30.md`, committed before the extract was pulled. Its primary pair, `ensemble-ratio` vs `percentile-tuned` under `every`, passed; the outcome and its caveats are at the end of that file.

## ensemble-held

The report split is held out from tuning; the hypothesis was formed after the first report run, so it is not held out from hypothesis formation. Its grid and the frozen `ensemble-held` block were committed before the report split was replayed.

Budget for every comparison of `ensemble-held`: the report-split `window` row of the same actuator times 1.10. Under `every` that is 1.2052 exceedance windows and 0.4625 limit changes per collection-day; the candidate has 0.0768 and 0.3656, inside both. Under `updater` it is 2.6251 and 0.0908; the candidate has 0.3185, inside, and 0.1823 changes, outside, so no updater comparison of `ensemble-held` is within budget.

Against `percentile-tuned` under `every` it has no slack advantage: +0.0069 [-0.0019, +0.0160] at 0.24x the exceedance windows, 0.96x the limit changes and 12.1% more memory-time.

`cmd/recommender` runs the same selector configuration with `--ensemble-fall-fraction 0.05 --ensemble-fall-windows 288 --ensemble-overrun-weight 20 --ensemble-limit-change-weight 0` at the default 5-minute `--memory-window`.

## ensemble-ratio

Same disclosure as `ensemble-held`: the report split is held out from tuning, not from hypothesis formation; the grid and the frozen `ensemble-ratio` block were committed before the report split was replayed. The grid's hysteresis values are `ensemble-held`'s chosen ones, not re-swept.

Budget, the report-split `window` row of the same actuator times 1.10. Under `every` (1.2052 exceedance windows and 0.4625 limit changes per collection-day) the candidate has 0.1266 and 0.3729, inside both. Under `updater` (2.6251 and 0.0908) it has 2.1386, inside, and 0.2247 changes, outside, so no updater comparison of `ensemble-ratio` is within budget.

Against `percentile-tuned` under `every` it has lower mean slack (-0.0220, bootstrap 95% CI [-0.0307, -0.0123]) at 0.39x the exceedance windows, 0.98x the limit changes and 6.9% more memory-time. Against `ensemble-held`, the recommender it is built on, it has lower slack under `every` (-0.0289 [-0.0366, -0.0207]) at 1.65x the exceedance windows and 4.6% less memory-time; under `updater` -0.0298 [-0.0379, -0.0210] at 6.7x the exceedance windows. Without the zero-margin arms this is the reverse of the earlier run, where the learned margin cost slack and saved exceedance windows.

`cmd/recommender` runs the same selector configuration with `--ensemble-ratio-windows 576 --ensemble-ratio-floor 1.0 --ensemble-fall-fraction 0.05 --ensemble-fall-windows 288 --ensemble-overrun-weight 50 --ensemble-limit-change-weight 0` at the default 5-minute `--memory-window`.

## Definitions

Windows are the trace's five-minute windows; one peak per instance per window from `maximum_usage.memory`, rows with it missing or zero dropped (`skipped_rows`). The limit scored for a window is the one in force before the window was observed. The first 48 h of every collection and the rest of the trace day they end in are not scored, for every recommender alike, so every scored collection-day is a whole trace day. Memory is a fraction of the largest machine, never bytes.

- Relative slack per collection-day: (mean limit over the day's windows minus the nearest-rank p95 of the instance-windows' average usage) divided by the mean limit. `mean_relative_slack` is the mean over collections of each collection's mean over its days.
- Limit-exceedance windows (simulated OOM proxy): instance-windows whose peak is above the limit, per collection-day. Never an OOM: the replay is open loop. For `assigned` under `every` each row is checked against its own `assigned_memory` before rows merge into instance-windows; its limit for slack is the window's duration-weighted mean, rows without `assigned_memory` left out.
- Limit changes per collection-day: windows whose limit differs from the previous window's.
- `memory_time`: sum over scored instance-windows of limit times seconds.
- Tracking: mean over windows of |limit - request| / request, where the request is the mean, over the instances whose first event has passed, of `resource_request.memory` rebuilt from every `instance_events` row that changes it, at lags 0, 1, 2 and 12 windows; the same over the 12 windows after each recorded update; and the fraction of updates after which the limit moved the same way within 12 windows. Reported for the FULLY_AUTOMATED and the OFF group.
- Comparison: paired difference in mean slack with a bootstrap 95% CI over groups (user and logical name), 2000 resamples, seed 1; win fraction is the share of collections where the candidate's slack is lower and its exceedance per day not higher.

## Tune and report split

Within cell a, by the low bit of FNV-1a over `user + "/" + logical_name` (the collection id when either is empty): 196 collections tune, 203 report.

## How the recommenders work

A VPA opts in by naming a recommender:

```yaml
spec:
  recommenders:
    - name: plimsoll           # or plimsoll-ensemble
```

The binary reads memory usage from metrics-server, watches for `OOMKilled` restarts, and writes `status.recommendation` through the status subresource, guarded by the VPA's UID and resourceVersion and capped by its memory `minAllowed`/`maxAllowed`. Upstream's recommender skips VPAs that name another recommender and this one skips the rest; upstream's updater and admission controller apply the result unchanged. State is saved in upstream's `VerticalPodAutoscalerCheckpoint` objects: the percentile histogram in the CRD's own fields, the ensemble's selector and arms in an annotation on the same object.

`plimsoll-ensemble` follows the paper: arg-min models, each a decayed histogram read at the limit that minimizes a weighted overrun and underrun cost, and a selector that picks the model whose recent limits would have cost least, with the paper's moving-window recommender as one more model. It departs from equations 4 to 8 in three ways: models are scored on the limit they held before a window, costs are divided by the window's task count, and margins multiply the limit. `ensemble-ratio` replaces the fixed margins with one learned per arm from recent peak-to-limit ratios and lets a recommendation fall only after the selector's recent max has stayed 5% lower for a day.

## Headline runs (moved from the top-level README)

Restart on kind, one run: steady 384 MiB workloads, 15 s windows, every recommender restarted at 1260 s and sampled for 660 s more (`results/summary/restart-2026-10-30/`).

| side | target before restart, MiB | lowest after, MiB | loss |
| --- | --- | --- | --- |
| percentile | 454 | 454 | 0% |
| ensemble | 494 | 494 | 0% |
| ensemble, checkpoints off | 494 | 415 | 16.0%, not recovered |
| upstream | 454 | 454 | 0% |

Actuated savings on kind, one run: four steady workloads with two replicas each, started at 1.97x to 3.96x their measured peak with memory request equal to limit, 27 min in `Off` then 20 min in `InPlaceOrRecreate` under upstream's updater and admission controller (`results/summary/savings-2026-10-30/summary.md`). Each resize restarted its container; none evicted a pod. The percentage reclaimed is set mostly by the starting over-provisioning.

| recommender | requested MiB before | after | reclaimed | OOM kills | in-place resizes | pod replacements |
| --- | --- | --- | --- | --- | --- | --- |
| plimsoll (percentile) | 8192 | 3617 | 55.9% | 0 | 8 | 0 |
| plimsoll-ensemble | 8192 | 3302 | 59.7% | 0 | 8 | 0 |
| ensemble-ratio | 8192 | 3617 | 55.9% | 0 | 8 | 0 |
| upstream | 8192 | 3617 | 55.9% | 0 | 8 | 0 |

Replay on a held-out cell. Every parameter was tuned on half of cell a; the primary comparison and its pass rule were committed before cell e was pulled (`results/summary/protocol-celle-2026-10-30.md`). Cell e: 312 long-running prod-tier collections of 2 to 50 tasks in 306 user/logical-name groups, 9,048 scored collection-days, 31.8 M instance-windows of five-minute memory peaks; the limit scored for a window is the one set before that window was seen. Each new target is applied (`every`). Runs `results/summary/celle-2026-10-30-*`, comparisons in `celle-2026-10-30-comparisons/`; 18 runs in 175 s.

| recommender | mean relative slack | vs tuned, 95% CI | limit-exceedance windows / collection-day | limit changes / collection-day | memory-time vs tuned |
| --- | --- | --- | --- | --- | --- |
| percentile, upstream defaults | 0.243 | | 4.05 | 0.86 | -2.5% |
| percentile, tuned (baseline) | 0.256 | | 0.64 | 0.41 | 0% |
| moving window (paper) | 0.226 | -0.0291 [-0.0373, -0.0208] | 0.93 | 0.29 | -4.9% |
| arg-min models only (paper) | 0.243 | -0.0127 [-0.0233, -0.0017] | 0.97 | 0.06 | -3.8% |
| ensemble | 0.243 | -0.0123 [-0.0225, -0.0016] | 0.90 | 0.07 | -4.3% |
| ensemble-ratio | 0.237 | -0.0189 [-0.0256, -0.0122] | 0.13 | 0.37 | +3.7% |
| 48 h max + 15% | 0.250 | | 0.05 | 1.97 | +2.5% |
| trace's own limit | 0.523 | | 15.60 | 2.25 | +56.6% |

Slack is (mean limit minus p95 usage) over mean limit per collection-day, averaged over collections; the CI is a paired bootstrap over groups. Exceedance is limit-exceedance windows (simulated OOM proxy), not OOM kills. The pre-registered test, ensemble-ratio against the tuned baseline, passed both gates: slack CI below zero and total exceedance windows no higher (0.196x). It costs 3.7% more memory-time: slack is averaged per collection, memory-time is summed and dominated by the largest ones. The exceedance total is concentrated: the baseline's 10 worst collections hold 5,210 of its 5,779 windows, and per collection ensemble-ratio exceeds more often on 228 of 312. Under a simplified form of upstream's updater (change at 10% drift, at most every 12 h) it is not confirmed: 1.64x the exceedance windows. On cell a's report half, where it was developed, the same pair was -0.0220 [-0.0307, -0.0123] at 0.39x the exceedance windows and +6.9% memory-time (`results/summary/cella-2026-10-30-comparisons/`).
