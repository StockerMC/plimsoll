# plimsoll

A Kubernetes VPA custom recommender for container memory, in Go. It serves a port of upstream VPA's decaying-histogram percentile recommender and a reimplementation of the recommender from the Autopilot paper (Rzadca et al., EuroSys '20), both checkpointed so a restart keeps their targets. The same `Recommender` interface drives the live binary and a replay of the Borg 2019 trace, so the model that writes VPA status on kind is the one scored on the trace.

## How it plugs into VPA

A VPA opts in by naming a recommender:

```yaml
spec:
  recommenders:
    - name: plimsoll           # or plimsoll-ensemble
```

The binary reads memory usage from metrics-server, watches for `OOMKilled` restarts, and writes `status.recommendation` through the status subresource, guarded by the VPA's UID and resourceVersion and capped by its memory `minAllowed`/`maxAllowed`. Upstream's recommender skips VPAs that name another recommender and this one skips the rest; upstream's updater and admission controller apply the result unchanged. State is saved in upstream's `VerticalPodAutoscalerCheckpoint` objects: the percentile histogram in the CRD's own fields, the ensemble's selector and arms in an annotation on the same object.

`plimsoll-ensemble` follows the paper: arg-min models, each a decayed histogram read at the limit that minimizes a weighted overrun and underrun cost, and a selector that picks the model whose recent limits would have cost least, with the paper's moving-window recommender as one more model. It departs from equations 4 to 8 in three ways: models are scored on the limit they held before a window, costs are divided by the window's task count, and margins multiply the limit. `ensemble-ratio` replaces the fixed margins with one learned per arm from recent peak-to-limit ratios and lets a recommendation fall only after the selector's recent max has stayed 5% lower for a day.

## Running

Needs Go, Docker, kind and kubectl.

```
make kind-up                          # kind cluster, metrics-server, upstream VPA 1.8.1
make demo                             # memory-load pod with VPAs for ours and upstream's; DEMO=oom or DEMO=ensemble
scripts/restart.sh <run>              # restart every recommender mid-run
make savings SAVINGS=<run>            # upstream updater and admission, actuated savings run
RUN=celle-<run> CELL=e make replay STEP=report   # Borg replay; needs the extract (data/README.md)
make test
```

## Results

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

## Limitations

- Memory only, no CPU.
- The replay is open loop: a limit never changes the usage it is scored against, and Borg usage is already capped by the real limit, so any max-plus-margin rule partly tracks it by construction.
- Cell e is another cell of the same trace month; 38 of its 306 groups share a user and logical name with cell a. The ensemble-ratio idea came after earlier cell a report runs.
- Trace memory is a fraction of an unpublished machine size, so no replay result is in bytes.
- Live results are single runs on kind with steady synthetic loads and upstream VPA 1.8.1. Requested memory is not physical memory, nodes or cost.

## Related work

The [Autopilot paper](https://dl.acm.org/doi/10.1145/3342195.3387524) (Rzadca et al., EuroSys '20) describes the moving-window and ML recommenders reimplemented here. [Escra](https://hunhoffe.github.io/assets/pdf/escra.pdf) (section VI-A) recreated the same recommender as a baseline. Upstream [VPA](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler) is the source of the percentile port, the checkpoint CRD and the `spec.recommenders` hook; Red Hat's predictive-vpa-recommenders use the same hook with Python forecasters.

## License

Apache-2.0. Parts are ported from upstream Kubernetes VPA, also Apache-2.0.
