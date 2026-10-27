# plimsoll

A Kubernetes VPA custom recommender in Go for container memory. It ports upstream VPA's decaying-histogram percentile recommender, including the OOM bump, and runs next to upstream's recommender.

## How it plugs into VPA

A VPA object opts in by naming the recommender:

```yaml
spec:
  recommenders:
    - name: plimsoll
```

The binary reads memory usage from metrics-server, watches containers for `OOMKilled` restarts, and writes `status.recommendation` through the status subresource. Upstream's recommender ignores VPAs that name another recommender, and this one ignores VPAs that do not name it.

## Quickstart

Needs Go, Docker, kind and kubectl.

```
make kind-up     # kind cluster, metrics-server, upstream VPA 1.8.1 CRDs and recommender
make demo        # memory-load pod with two VPAs, one ours and one upstream's; prints both targets for 10 min
make scenarios   # side-by-side scenario suite, about 33 min; writes results/summary/scenarios-<date>/
make test        # go vet and go test
```

`make demo DEMO=oom` runs a pod whose limit is below its peak, so it is OOMKilled and the target rises.

## Scenario results

Two identical Deployments per scenario, one VPA naming this recommender and one left to upstream's, both in `Off` mode. Ours runs with a 1-minute memory window and otherwise upstream's defaults. Targets are sampled every 15 s. From `results/summary/scenarios-2026-10-27/`.

| scenario | recommender | final target (MiB) | min target (MiB) | OOM kills | settle (s) |
| --- | --- | --- | --- | --- | --- |
| steady | ours | 454 | 250 | 0 | 180 |
| steady | upstream | 454 | 309 | 0 | 120 |
| burst | ours | 600 | 250 | 1 | 435 |
| burst | upstream | 600 | 250 | 1 | 405 |
| restart | ours | 454 | 250 | 0 | 120 |
| restart | upstream | 454 | 250 | 0 | 0 |
| missing-metrics | ours | 454 | 250 | 0 | 0 |
| missing-metrics | upstream | 454 | 250 | 0 | 0 |

- steady: ramp to 384 MiB and hold. burst: 128 MiB base with spikes of +128, +256, +384 MiB against a 512 Mi limit. restart: both recommenders restarted at 4 min. missing-metrics: metrics-server scaled to 0 from 3 to 6 min.
- Settle is seconds from the last event (or the start) until the target stays within 10% of its final value.
- Ours rises 30 to 90 s after upstream, since a peak enters the histogram only when its 1-minute window closes.
- OOM kills are a property of the workload, the same on both sides, since the VPAs are `Off`. After the kill both targets end at 600 MiB.
- During the metrics-server outage neither target moves: ours logs the error and skips the patch, upstream keeps its last status.

## Limitations

- Memory only. No CPU.
- No checkpoints. A restart is a cold start: in the restart scenario our target fell to the 250 MiB floor for about 2 min, while upstream's status did not move.
- Recommends only. No updater or admission-controller changes; actuation is upstream's.
- Tested only on kind with upstream VPA 1.8.1.
- The scenarios are single runs of minutes, not a benchmark.

## License

Apache-2.0. Parts are ported from upstream Kubernetes VPA, also Apache-2.0.
