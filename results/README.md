# results

`results/raw/` is not committed: per-collection series and logs. `results/summary/` is: one directory per replay run, the sweep table, the comparisons, and the kind scenario runs.

## Replay runs

`scripts/replay-all.sh sweep` replays every entry of `configs/sweep-cella.json` on the tune split and writes `sweep-cella.csv`, one row per run; `sweep-beyond.csv` is the same for `configs/sweep-beyond.json`, the fall-hysteresis grid, and `sweep-ratio.csv` for `configs/sweep-ratio.json`, the learned-margin grid. The window reference (p98, 48 h half-life, 12-window peak and max, 15%/10% margin) is never swept. A row is eligible when its exceedance windows and limit changes per collection-day are each within 10% of the reference's; the chosen row per recommender is the eligible one with the lowest mean slack. The chosen values are copied into `configs/default.json` by hand and committed before the report split is run.

`scripts/replay-all.sh report` replays the report split with every recommender under both actuation rules and writes `cella-<date>-<recommender>-<actuator>/summary.json` and `collections.csv` (one row per collection: the summary's inputs). `scripts/replay-all.sh compare` writes `compare-<date>/<candidate>-vs-<baseline>.{json,md}` for the pre-registered pairs.

Recommenders: `percentile` (the upstream VPA port at upstream's defaults), `percentile-tuned` (the sweep's pick), `window` (the frozen reference), `ml` (arg-min models only, the configuration of the Autopilot paper), `ensemble` (the same selector with the window reference as one more arm), `ensemble-held` (the ensemble whose recommendation follows a fall in the selector's hourly max only after it has stayed at least `fall_fraction` lower for `fall_windows` windows in a row, rises followed at once; its weights and the two thresholds are the `ensemble-held` block of `configs/default.json`, chosen by `sweep-beyond.csv`), `ensemble-ratio` (the held ensemble with one arg-min arm per half-life whose margin is learned instead of the grid of fixed margins: each arm's limit is its raw limit times the largest ratio of a window's peak to the raw limit the arm held before that window over its last `ratio_windows` windows, at least `ratio_floor`; the window arm keeps its step margin; parameters in the `ensemble-ratio` block of `configs/default.json`, chosen by `sweep-ratio.csv`), `assigned` (the trace's own `assigned_memory`, the sanity check for the metric), `naive` (the largest peak of the last 48 h plus 15%, the tracking control).

Actuators: `every` sets the limit to the target whenever it changes. `updater` is a simplified form of upstream VPA's updater: change when the request is outside the recommendation's bounds, or differs from the target by 10% or more and the last change is at least 12 h old (upstream checks pod age instead); the request starts at the trace's `assigned_memory` of the collection's first window. `window`, `ml`, `ensemble`, `ensemble-held`, `ensemble-ratio`, `assigned` and `naive` report bounds equal to their target, for which the bounds test would fire at every drift, so for them only the 10%/12 h rule applies.

## ensemble-held

The report split is held out from tuning; the hypothesis was formed after the first report run, so it is not held out from hypothesis formation. Its grid and the frozen `ensemble-held` block were committed before the report split was replayed.

Budget for every comparison of `ensemble-held`: the report-split `window` row of the same actuator times 1.10. Under `every` that is 1.1656 exceedance windows and 0.4473 limit changes per collection-day; the candidate has 0.1452 and 0.3591, inside both. Under `updater` it is 2.5390 and 0.0880; the candidate has 2.4120, inside, and 0.2594 changes, outside, so no updater comparison of `ensemble-held` is within budget.

`cmd/recommender` runs the same selector configuration with `--ensemble-fall-fraction 0.1 --ensemble-fall-windows 288 --ensemble-overrun-weight 100 --ensemble-limit-change-weight 0` at the default 5-minute `--memory-window`.

## ensemble-ratio

Same disclosure as `ensemble-held`: the report split is held out from tuning, not from hypothesis formation; the grid and the frozen `ensemble-ratio` block were committed before the report split was replayed. The grid's hysteresis values are `ensemble-held`'s chosen ones, not re-swept.

Budget, the report-split `window` row of the same actuator times 1.10. Under `every` (1.1656 exceedance windows and 0.4473 limit changes per collection-day) the candidate has 0.1166 and 0.3527, inside both. Under `updater` (2.5390 and 0.0880) it has 0.7817, inside, and 0.2182 changes, outside, so no updater comparison of `ensemble-ratio` is within budget.

Against `ensemble-held`, the recommender it is built on, it has higher mean slack under `every` (+0.0123, bootstrap 95% CI [+0.0037, +0.0209]) at 0.80x the exceedance windows and 0.98x the limit changes; under `updater` the slack difference is +0.0039 [-0.0051, +0.0126] at 0.32x the exceedance windows. The learned margin trades slack for fewer exceedance windows; it does not lower slack.

`cmd/recommender` runs the same selector configuration with `--ensemble-ratio-windows 288 --ensemble-ratio-floor 1.05 --ensemble-fall-fraction 0.1 --ensemble-fall-windows 288 --ensemble-overrun-weight 5 --ensemble-limit-change-weight 0` at the default 5-minute `--memory-window`.

## Definitions

Windows are the trace's five-minute windows; one peak per instance per window from `maximum_usage.memory`, rows with it missing or zero dropped (`skipped_rows`). The limit scored for a window is the one in force before the window was observed. The first 48 h of every collection are not scored, for every recommender alike. Memory is a fraction of the largest machine, never bytes.

- Relative slack per collection-day: (mean limit over the day's windows minus the nearest-rank p95 of the instance-windows' average usage) divided by the mean limit. `mean_relative_slack` is the mean over collections of each collection's mean over its days.
- Limit-exceedance windows (simulated OOM proxy): instance-windows whose peak is above the limit, per collection-day. Never an OOM: the replay is open loop. For `assigned` under `every` each row is checked against its own `assigned_memory` before rows merge into instance-windows; its limit for slack is the window's duration-weighted mean, rows without `assigned_memory` left out.
- Limit changes per collection-day: windows whose limit differs from the previous window's.
- `memory_time`: sum over scored instance-windows of limit times seconds.
- Tracking: mean over windows of |limit - request| / request, where the request is the mean, over the instances whose first event has passed, of `resource_request.memory` rebuilt from every `instance_events` row that changes it, at lags 0, 1, 2 and 12 windows; the same over the 12 windows after each recorded update; and the fraction of updates after which the limit moved the same way within 12 windows. Reported for the FULLY_AUTOMATED and the OFF group.
- Comparison: paired difference in mean slack with a bootstrap 95% CI over groups (user and logical name), 2000 resamples, seed 1; win fraction is the share of collections where the candidate's slack is lower and its exceedance per day not higher.

## Tune and report split

Within cell a, by the low bit of FNV-1a over `user + "/" + logical_name` (the collection id when either is empty): 196 collections tune, 203 report.
