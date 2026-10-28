# results

`results/raw/` is not committed: per-collection series and logs. `results/summary/` is: one directory per replay run, the sweep table, the comparisons, and the kind scenario runs.

## Replay runs

`scripts/replay-all.sh sweep` replays every entry of `configs/sweep-cella.json` on the tune split and writes `sweep-cella.csv`, one row per run. The window reference (p98, 48 h half-life, 12-window peak and max, 15%/10% margin) is never swept. A row is eligible when its exceedance windows and limit changes per collection-day are each within 10% of the reference's; the chosen row per recommender is the eligible one with the lowest mean slack. The chosen values are copied into `configs/default.json` by hand and committed before the report split is run.

`scripts/replay-all.sh report` replays the report split with every recommender under both actuation rules and writes `cella-<date>-<recommender>-<actuator>/summary.json` and `collections.csv` (one row per collection: the summary's inputs). `scripts/replay-all.sh compare` writes `compare-<date>/<candidate>-vs-<baseline>.{json,md}` for the pre-registered pairs.

Recommenders: `percentile` (the upstream VPA port at upstream's defaults), `percentile-tuned` (the sweep's pick), `window` (the frozen reference), `ml` (arg-min models only, the configuration of the Autopilot paper), `ensemble` (the same selector with the window reference as one more arm), `assigned` (the trace's own `assigned_memory`, the sanity check for the metric), `naive` (the largest peak of the last 48 h plus 15%, the tracking control).

Actuators: `every` sets the limit to the target whenever it changes. `updater` applies upstream VPA's updater rule: change when the request is outside the recommendation's bounds, or differs from the target by 10% or more and the last change is at least 12 h old; the request starts at the trace's `assigned_memory` of the collection's first window. Every recommender but `percentile` reports bounds equal to its target, for which the bounds test would fire at every drift, so for them only the 10%/12 h rule applies.

## Definitions

Windows are the trace's five-minute windows; one peak per instance per window from `maximum_usage.memory`, rows with it missing or zero dropped (`skipped_rows`). The limit scored for a window is the one in force before the window was observed. The first 48 h of every collection are not scored, for every recommender alike. Memory is a fraction of the largest machine, never bytes.

- Relative slack per collection-day: (mean limit over the day's windows minus the nearest-rank p95 of the instance-windows' average usage) divided by the mean limit. `mean_relative_slack` is the mean over collections of each collection's mean over its days.
- Limit-exceedance windows (simulated OOM proxy): instance-windows whose peak is above the limit, per collection-day. Never an OOM: the replay is open loop.
- Limit changes per collection-day: windows whose limit differs from the previous window's.
- `memory_time`: sum over scored instance-windows of limit times seconds.
- Tracking: mean over windows of |limit - request| / request, where the request is the mean, over the instances whose first event has passed, of `resource_request.memory` rebuilt from every `instance_events` row that changes it, at lags 0, 1, 2 and 12 windows; the same over the 12 windows after each recorded update; and the fraction of updates after which the limit moved the same way within 12 windows. Reported for the FULLY_AUTOMATED and the OFF group.
- Comparison: paired difference in mean slack with a bootstrap 95% CI over groups (user and logical name), 2000 resamples, seed 1; win fraction is the share of collections where the candidate's slack is lower and its exceedance per day not higher.

## Tune and report split

Within cell a, by the low bit of FNV-1a over `user + "/" + logical_name` (the collection id when either is empty): 196 collections tune, 203 report.
