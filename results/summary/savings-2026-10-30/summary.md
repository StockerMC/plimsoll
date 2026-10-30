Requested memory on synthetic kind workloads with memory request = limit; it says nothing about physical memory, nodes or cost. Before: the last fully Ready sample in updateMode Off. After: the first sample within 15 s from the scheduled end, nothing later counted, and only if the desired replicas never changed, every one is Ready and applied resources equal spec with no resize pending. OOM kills: distinct OOMKilled terminations after the switch; a trailing + marks a lower bound (sampling gap, termination watch down while a pod was deleted, or a restart with no termination record on its own pod). Readiness loss counts 15 s samples with fewer Ready replicas than desired, so a restart shorter than the sampling interval can go unseen. Usage is the peak per-pod working set from metrics-server during updateMode Off; provisioned x is requested memory over that usage.

| recommender | workloads | requested MiB before | after | reclaimed % | reclaimed MiB vs upstream | OOM kills | OOM kills in Off | completed resizes | container restarts | pod replacements | sampled readiness loss s | exposure pod-min |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| plimsoll | 4 | 8192 | 3617 | 55.9 | 0 | 0 | 0 | 8 | 8 | 0 | 0 | 160 |
| ensemble | 4 | 8192 | 3302 | 59.7 | 314 | 0 | 0 | 8 | 8 | 0 | 0 | 160 |
| ratio | 4 | 8192 | 3617 | 55.9 | 0 | 0 | 0 | 8 | 8 | 0 | 0 | 160 |
| upstream | 4 | 8192 | 3617 | 55.9 | 0 | 0 | 0 | 8 | 8 | 0 | 0 | 160 |

| side | workload | replicas | requested_mib_before | requested_mib_after | reclaimed_pct | target_mib | off_usage_mib | provisioned_x_before | provisioned_x_after | oom_kills | completed_resizes | container_restarts | pod_replacements | sampled_readiness_loss_s | exposure_pod_min |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| plimsoll | s288 | 2 | 1152 | 725 | 37.0 | 363 | 292 | 1.97 | 1.24 | 0 | 2 | 2 | 0 | 0 | 40 |
| plimsoll | s384 | 2 | 3072 | 909 | 70.4 | 454 | 388 | 3.96 | 1.17 | 0 | 2 | 2 | 0 | 0 | 40 |
| plimsoll | s320 | 2 | 1920 | 783 | 59.2 | 392 | 324 | 2.96 | 1.21 | 0 | 2 | 2 | 0 | 0 | 40 |
| plimsoll | s512 | 2 | 2048 | 1199 | 41.4 | 600 | 516 | 1.98 | 1.16 | 0 | 2 | 2 | 0 | 0 | 40 |
| ensemble | s288 | 2 | 1152 | 662 | 42.5 | 331 | 292 | 1.97 | 1.13 | 0 | 2 | 2 | 0 | 0 | 40 |
| ensemble | s384 | 2 | 3072 | 830 | 73.0 | 415 | 388 | 3.96 | 1.07 | 0 | 2 | 2 | 0 | 0 | 40 |
| ensemble | s320 | 2 | 1920 | 715 | 62.7 | 358 | 324 | 2.96 | 1.10 | 0 | 2 | 2 | 0 | 0 | 40 |
| ensemble | s512 | 2 | 2048 | 1095 | 46.5 | 547 | 517 | 1.98 | 1.06 | 0 | 2 | 2 | 0 | 0 | 40 |
| ratio | s288 | 2 | 1152 | 725 | 37.0 | 363 | 292 | 1.97 | 1.24 | 0 | 2 | 2 | 0 | 0 | 40 |
| ratio | s384 | 2 | 3072 | 909 | 70.4 | 454 | 388 | 3.96 | 1.17 | 0 | 2 | 2 | 0 | 0 | 40 |
| ratio | s320 | 2 | 1920 | 783 | 59.2 | 392 | 324 | 2.96 | 1.21 | 0 | 2 | 2 | 0 | 0 | 40 |
| ratio | s512 | 2 | 2048 | 1199 | 41.4 | 600 | 517 | 1.98 | 1.16 | 0 | 2 | 2 | 0 | 0 | 40 |
| upstream | s288 | 2 | 1152 | 725 | 37.0 | 363 | 292 | 1.97 | 1.24 | 0 | 2 | 2 | 0 | 0 | 40 |
| upstream | s384 | 2 | 3072 | 909 | 70.4 | 454 | 388 | 3.96 | 1.17 | 0 | 2 | 2 | 0 | 0 | 40 |
| upstream | s320 | 2 | 1920 | 783 | 59.2 | 392 | 324 | 2.96 | 1.21 | 0 | 2 | 2 | 0 | 0 | 40 |
| upstream | s512 | 2 | 2048 | 1199 | 41.4 | 600 | 517 | 1.98 | 1.16 | 0 | 2 | 2 | 0 | 0 | 40 |
