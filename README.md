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
make replay STEP=report   # Borg replay; needs the extract in data/ (data/README.md)
make test
```

## Scenario results

Three identical Deployments per scenario, VPAs in `Off` mode naming `plimsoll`, `plimsoll-ensemble` and upstream's recommender. Ours run with a 1-minute memory window. Targets in MiB, sampled every 15 s, from `results/summary/scenarios-2026-10-28/`.

| scenario | final: ours / ensemble / upstream | min: ours / ensemble / upstream | OOM kills (each side) | settle s: ours / ensemble / upstream |
| --- | --- | --- | --- | --- |
| steady | 454 / 454 / 454 | 250 / 250 / 309 | 0 | 195 / 195 / 120 |
| burst | 600 / 600 / 600 | 250 / 250 / 309 | 1 | 435 / 495 / 405 |
| restart | 454 / 454 / 454 | 250 / 250 / 250 | 0 | 0 / 75 / 0 |
| missing-metrics | 454 / 454 / 454 | 250 / 250 / 309 | 0 | 0 / 0 / 0 |

- steady: ramp to 384 MiB and hold. burst: 128 MiB base with spikes of +128, +256, +384 MiB against a 512 Mi limit. restart: all recommenders restarted at 4 min. missing-metrics: metrics-server scaled to 0 from 3 to 6 min.
- Settle is seconds from the last event (or the start) until the target stays within 10% of its final value. Minimums of 250 are the floor during warm-up.
- On restart ours reloads its checkpoint from upstream's `VerticalPodAutoscalerCheckpoint` CRD and its target does not move; the ensemble restarts cold, was at the floor from 240 through 300 s and was back by 315 s.
- No scenario outlasts the ensemble's 12-window cold start, so its rows are its cold-start rule, not the selector.
- OOM kills are a property of the workload, the same on every side, since the VPAs are `Off`.

## Replay on the Borg 2019 trace

`cmd/replay` drives every recommender through the live binary's interface over five-minute memory peaks from cell a of the Borg 2019 trace: 399 long-running prod-tier collections of 2 to 50 tasks, split by a hash of user and logical name into 196 for tuning and 203 for reporting. The limit scored for a window is the one set before that window was observed. Each recommender runs under two actuation rules: `every` applies each new target, `updater` is a simplified form of upstream's updater (10% and 12 h). Report split, 5,887 collection-days, each a whole trace day after a 48 h cold start; definitions and every run in `results/README.md`.

| recommender | slack, every | exceedance/day, every | changes/day, every | slack, updater | exceedance/day, updater | changes/day, updater |
| --- | --- | --- | --- | --- | --- | --- |
| percentile, upstream defaults | 0.274 | 1.63 | 0.81 | 0.272 | 1.97 | 0.28 |
| percentile, tuned | 0.284 | 0.32 | 0.38 | 0.277 | 1.10 | 0.18 |
| moving window (paper) | 0.246 | 1.10 | 0.42 | 0.249 | 2.39 | 0.08 |
| ML selector, arg-min models only | 0.140 | 8.80 | 1.40 | 0.151 | 33.91 | 0.41 |
| ensemble | 0.140 | 8.71 | 1.42 | 0.152 | 33.82 | 0.42 |
| ensemble-held | 0.257 | 0.15 | 0.37 | 0.251 | 2.49 | 0.27 |
| ensemble-ratio | 0.270 | 0.12 | 0.36 | 0.255 | 0.81 | 0.23 |
| 48 h max + 15% | 0.270 | 0.08 | 1.77 | 0.265 | 0.77 | 0.22 |
| trace's own limit | 0.438 | 1.90 | 2.05 | 0.437 | 3.83 | 0.09 |

Slack is mean relative slack over collections, exceedance is limit-exceedance windows (simulated OOM proxy) per collection-day, changes are limit changes per collection-day.

Under `every` the ensemble's mean relative slack is 14.4 points below the tuned percentile baseline (paired difference -0.144, bootstrap 95% CI over collection groups [-0.158, -0.130]) at 27x its exceedance windows, and it has lower slack at no more exceedance on 1% of collections.

Beyond the paper, `ensemble-held` lets the recommendation fall only after the selector's hourly max has stayed 10% lower for a day. Against the ensemble, the pre-registered comparison, it loses: 11.7 points more slack ([+0.106, +0.129]) for 0.017x its exceedance windows. Against the tuned baseline under `every` it cut mean relative slack by 2.7 points (0.257 vs 0.284, [-0.037, -0.016]) at 0.46x its exceedance windows, 0.98x its limit changes and 10.9% more memory-time, with lower slack at no more exceedance on 7% of collections. `ensemble-ratio` adds a margin per arm learned from that arm's peak-to-limit ratios and trades 1.2 points more slack ([+0.004, +0.021]) for 0.80x the held ensemble's exceedance windows; it also loses to the ensemble on slack (+0.130, [+0.120, +0.139]). Both were tuned on the tuning half only, but the ideas came after the first report run. Under `updater` neither stays within the moving window's limit-change budget.

On the 107 collections the trace marks `VERTICAL_SCALING_FULLY_AUTOMATED`, no recommender tracked the recorded request updates more closely than the 48 h running max (under `every`, mean absolute relative error against the previous window's request over the 12 windows after each update, averaged over collections: 0.189, against 0.191 for the upstream-default percentile and 0.245 for the ensemble). On the 96 collections with vertical scaling off, the same error against their recorded requests over all scored windows is 0.51 to 0.56 for every recommender.

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
