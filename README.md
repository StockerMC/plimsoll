# plimsoll

A Kubernetes VPA custom recommender in Go for container memory. It serves two recommenders: a port of upstream VPA's decaying-histogram percentile recommender, with the OOM bump and checkpoints, and a reimplementation of the recommender from the Autopilot paper (Rzadca et al., EuroSys '20). Both run next to upstream's recommender on kind and are replayed against one cell of the Borg 2019 trace.

## How it plugs into VPA

A VPA object opts in by naming a recommender:

```yaml
spec:
  recommenders:
    - name: plimsoll           # or plimsoll-ensemble
```

The binary reads memory usage from metrics-server, watches containers for `OOMKilled` restarts, and writes `status.recommendation` through the status subresource, capped by the VPA's memory `minAllowed` and `maxAllowed` and left out for containers in mode `Off` or without memory in `controlledResources`. Upstream's recommender ignores VPAs that name another recommender, and this one ignores VPAs that do not name it.

`plimsoll-ensemble` reimplements the paper's ML recommender: arg-min models, each a decayed histogram read at the limit that minimizes a weighted overrun and underrun cost, and a selector that picks the model whose recent limits would have cost least, with the paper's moving-window statistic added as one more model it can pick. It departs from the paper's equations 4 to 8 in three ways: each model is scored on the limit it held before a window rather than the limit fitted on it, costs are divided by the window's task count, and margins are a fraction of the limit rather than added to it. [Escra](https://hunhoffe.github.io/assets/pdf/escra.pdf) (section VI-A) recreated the same recommender as a baseline; this one is in Go and shares one interface between the live binary and the replay.

## Running

Needs Go, Docker, kind and kubectl.

```
make kind-up      # kind cluster, metrics-server, upstream VPA 1.8.1
make demo         # memory-load pod with VPAs for ours and upstream's; DEMO=oom or DEMO=ensemble
make scenarios    # side-by-side scenario suite
RUN=cella-<unique-id> make replay STEP=report    # Borg replay; needs the extract in data/ (data/README.md)
RUN=cella-<unique-id> make replay STEP=compare   # same RUN
make test
```

## Scenario results

Three identical Deployments per scenario, VPAs in `Off` mode naming `plimsoll`, `plimsoll-ensemble` and upstream's recommender. Ours run with a 1-minute memory window. Targets in MiB, sampled every 15 s, from `results/summary/scenarios-2026-10-30/` at 64bfc21.

| scenario | final: ours / ensemble / upstream | min: ours / ensemble / upstream | OOM kills (each side) | settle s: ours / ensemble / upstream |
| --- | --- | --- | --- | --- |
| steady | 454 / 454 / 454 | 392 / 392 / 250 | 0 | 180 / 180 / 120 |
| burst | 600 / 600 / 600 | 309 / 309 / 250 | 1 | 435 / 495 / 405 |
| restart | 454 / 454 / 454 | 392 / 309 / 250 | 0 | 0 / 60 / 0 |
| missing-metrics | 454 / 454 / 454 | 309 / 392 / 309 | 0 | 0 / 0 / 0 |

- steady: ramp to 384 MiB and hold. burst: 128 MiB base with spikes of +128, +256, +384 MiB against a 512 Mi limit. restart: all recommenders restarted at 4 min. missing-metrics: metrics-server scaled to 0 from 3 to 6 min.
- Settle is seconds from the last event (or the start) until the target stays within 10% of its final value. Ours write no status until a usage window, an OOM or a checkpoint backs the model, so their minimums are the first measured target; upstream's 250 is its floor during warm-up.
- On restart ours reloads its checkpoint from upstream's `VerticalPodAutoscalerCheckpoint` CRD and its target does not move. The ensemble has no checkpoint: it restarts cold but leaves its status alone until its first window closes, so it stayed at its pre-restart 309 through the restart and reached 454 at 300 s, the settle of 60 s.
- No scenario outlasts the ensemble's 12-window cold start, so its rows are its cold-start rule, not the selector.
- OOM kills are a property of the workload, the same on every side, since the VPAs are `Off`.

## Replay on the Borg 2019 trace

`cmd/replay` drives every recommender through the live binary's interface over five-minute memory peaks from cell a of the Borg 2019 trace: 399 long-running prod-tier collections of 2 to 50 tasks, split by a hash of user and logical name into 196 for tuning and 203 for reporting. The limit scored for a window is the one set before that window was observed. Each recommender runs under two actuation rules: `every` applies each new target, `updater` is a simplified form of upstream's updater (10% and 12 h). Report split, 5,887 collection-days, each a whole trace day after a 48 h cold start; definitions and every run in `results/README.md`.

| recommender | slack, every | exceedance/day, every | changes/day, every | slack, updater | exceedance/day, updater | changes/day, updater |
| --- | --- | --- | --- | --- | --- | --- |
| percentile, upstream defaults | 0.274 | 1.63 | 0.81 | 0.272 | 1.97 | 0.28 |
| percentile, tuned | 0.284 | 0.32 | 0.38 | 0.277 | 1.10 | 0.18 |
| moving window (paper) | 0.246 | 1.10 | 0.42 | 0.249 | 2.39 | 0.08 |
| ML selector, arg-min models only | 0.270 | 0.62 | 0.06 | 0.259 | 2.44 | 0.03 |
| ensemble | 0.273 | 0.53 | 0.08 | 0.261 | 2.12 | 0.04 |
| ensemble-held | 0.291 | 0.08 | 0.37 | 0.290 | 0.32 | 0.18 |
| ensemble-ratio | 0.262 | 0.13 | 0.37 | 0.260 | 2.14 | 0.22 |
| 48 h max + 15% | 0.270 | 0.08 | 1.77 | 0.265 | 0.77 | 0.22 |
| trace's own limit | 0.438 | 1.90 | 2.05 | 0.437 | 3.83 | 0.09 |

Slack is mean relative slack over collections, exceedance is limit-exceedance windows (simulated OOM proxy) per collection-day, changes are limit changes per collection-day. Runs `results/summary/cella-2026-10-30-*` at 64bfc21, comparisons in `cella-2026-10-30-comparisons/`.

Under `every` the ensemble's mean relative slack is 1.2 points below the tuned percentile baseline (paired difference -0.012, bootstrap 95% CI over collection groups [-0.023, +0.000]) at 1.6x its exceedance windows, 0.21x its limit changes and 9.2% more memory-time; the interval reaches zero, so there is no measured slack advantage. Under `updater` it is -0.016 [-0.028, -0.003] at 1.9x the exceedance windows and 8.3% more memory-time. The fixed-margin arms have no zero margin; with zero-margin arms (`cella-2026-10-28-*`) the ensemble had 14.4 points less slack than the tuned baseline at 27x its exceedance windows.

Beyond the paper, `ensemble-held` lets the recommendation fall only after the selector's hourly max has stayed 5% lower for a day. It loses: against the ensemble, the pre-registered comparison, 1.8 points more slack ([+0.007, +0.030]) for 0.14x its exceedance windows and 2.7% more memory-time; against the tuned baseline +0.007 [-0.002, +0.016] at 0.24x the exceedance windows and 12.1% more memory-time. `ensemble-ratio` adds a margin per arm learned from that arm's peak-to-limit ratios. Under `every` it has 2.2 points less slack than the tuned baseline ([-0.031, -0.012]) at 0.39x its exceedance windows, 0.98x its limit changes and 6.9% more memory-time, with lower slack at no more exceedance on 10% of collections; against `ensemble-held`, the pre-registered comparison, -0.029 [-0.037, -0.021] at 1.65x the exceedance windows and 4.6% less memory-time. Both were tuned on the tuning half only, but the ideas, and the removal of the zero-margin arms, came after earlier report runs. Under `updater` neither stays within the moving window's limit-change budget.

On the 107 collections the trace marks `VERTICAL_SCALING_FULLY_AUTOMATED`, no recommender tracked the recorded request updates more closely than the 48 h running max (under `every`, mean absolute relative error against the previous window's request over the 12 windows after each update, averaged over collections: 0.189, against 0.192 for the upstream-default percentile and 0.316 for the ensemble). On the 96 collections with vertical scaling off, the same error against their recorded requests over all scored windows is 0.51 to 0.53 for every recommender.

## Limitations

- Memory only. No CPU.
- The replay is open loop: a limit never changes the usage it is scored against, and Borg usage is already capped by the real limit (censored), so any max-plus-margin rule partly tracks the request by construction. Exceedance windows are a proxy, not OOM kills.
- Tuning and reporting are two halves of one cell, not separate cells.
- Trace memory is a fraction of an unpublished machine size, so no result is in bytes.
- Every side has a 250 MiB floor; scenario minimums of 250 are that floor, not the workload.
- Recommends only; actuation is upstream's. Tested only on kind with upstream VPA 1.8.1. The scenarios are single runs of minutes.

Related: Red Hat's predictive VPA recommenders (openshift/predictive-vpa-recommenders) use the same `spec.recommenders` hook with Python forecasters.

## License

Apache-2.0. Parts are ported from upstream Kubernetes VPA, also Apache-2.0.
