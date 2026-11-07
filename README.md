# plimsoll

A Kubernetes VPA custom recommender for container memory, in Go. It serves a port of upstream VPA's percentile recommender and a reimplementation of the recommender from the Autopilot paper (Rzadca et al., EuroSys '20), both checkpointed so a restart keeps their targets. The same code drives the live binary and a replay of the Borg 2019 trace.

## Use

A VPA opts in by naming a recommender:

```yaml
spec:
  recommenders:
    - name: plimsoll           # or plimsoll-ensemble
```

Upstream's updater and admission controller apply the result unchanged. The ensemble departs from the paper's equations 4 to 8 in three ways, listed with the design in [results/README.md](results/README.md#how-the-recommenders-work).

```
make kind-up    # kind cluster, metrics-server, upstream VPA 1.8.1
make demo       # memory-load pod with VPAs for ours and upstream's
make test
```

Replaying the trace needs the extract described in [data/README.md](data/README.md).

## Results

Held-out replay on Borg 2019 cell e: 312 long-running collections, 9,048 collection-days, parameters frozen on cell a before cell e was pulled. Each new target is applied.

| recommender | mean relative slack | limit-exceedance windows / collection-day | memory-time vs tuned |
| --- | --- | --- | --- |
| percentile, tuned (baseline) | 0.256 | 0.64 | 0% |
| ensemble | 0.243 | 0.90 | -4.3% |
| ensemble-ratio | 0.237 | 0.13 | +3.7% |
| 48 h max + 15% | 0.250 | 0.05 | +2.5% |

Exceedance windows are a simulated OOM proxy, not OOM kills. On kind, every recommender kept its target through a restart, and under upstream's updater the plimsoll recommenders reclaimed 56 to 60% of deliberately over-provisioned requests with no OOM kills. Full tables, confidence intervals and caveats: [results/README.md](results/README.md#headline-runs-moved-from-the-top-level-readme).

## Limitations

- Memory only.
- The replay is open loop, and trace usage is already capped by the real limit.
- Live results are single runs on kind with synthetic loads.

## Related work

[Autopilot](https://dl.acm.org/doi/10.1145/3342195.3387524) (EuroSys '20), [Escra](https://hunhoffe.github.io/assets/pdf/escra.pdf) section VI-A, upstream [VPA](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler), and Red Hat's predictive-vpa-recommenders.

## License

Apache-2.0. Parts are ported from upstream Kubernetes VPA, also Apache-2.0.
