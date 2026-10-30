Requested memory on synthetic kind workloads with memory request = limit; it says nothing about physical memory, nodes or cost. Before: the last fully Ready sample in updateMode Off. After: the sample at the scheduled end, counted only if the desired replicas never changed, every one is Ready and applied resources equal spec with no resize pending. OOM kills: distinct OOMKilled terminations after the switch; a trailing + marks a lower bound (sampling gap, termination watch down while a pod was deleted, or a restart with no termination record on its own pod). Readiness loss counts 15 s samples with fewer Ready replicas than desired, so a restart shorter than the sampling interval can go unseen. Usage is the peak per-pod working set from metrics-server during updateMode Off; provisioned x is requested memory over that usage.

| recommender | workloads | requested MiB before | after | reclaimed % | reclaimed MiB vs upstream | OOM kills | OOM kills in Off | completed resizes | container restarts | pod replacements | sampled readiness loss s | exposure pod-min |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| plimsoll | 4 | 9344 | 3617 | 61.3 | 0 | 0+ | 0 | 8 | 8 | 0 | 0 | 160 |
| ensemble | 4 | 9344 | 3145 | 66.3 | 472 | 0+ | 0 | 8 | 8 | 0 | 0 | 160 |
| held | 4 | 9344 | 3617 | 61.3 | 0 | 0+ | 0 | 8 | 8 | 0 | 0 | 160 |
| upstream | 4 | 9344 | 3617 | 61.3 | 0 | 0+ | 0 | 8 | 8 | 0 | 0 | 160 |

| side | workload | replicas | requested_mib_before | requested_mib_after | reclaimed_pct | target_mib | off_usage_mib | provisioned_x_before | provisioned_x_after | oom_kills | completed_resizes | container_restarts | pod_replacements | sampled_readiness_loss_s | exposure_pod_min |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| plimsoll | s288 | 2 | 2304 | 725 | 68.5 | 363 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| plimsoll | s384 | 2 | 3072 | 909 | 70.4 | 454 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| plimsoll | s320 | 2 | 1920 | 783 | 59.2 | 392 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| plimsoll | s512 | 2 | 2048 | 1199 | 41.4 | 600 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| ensemble | s288 | 2 | 2304 | 631 | 72.6 | 315 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| ensemble | s384 | 2 | 3072 | 790 | 74.3 | 395 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| ensemble | s320 | 2 | 1920 | 681 | 64.5 | 341 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| ensemble | s512 | 2 | 2048 | 1043 | 49.1 | 521 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| held | s288 | 2 | 2304 | 725 | 68.5 | 363 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| held | s384 | 2 | 3072 | 909 | 70.4 | 454 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| held | s320 | 2 | 1920 | 783 | 59.2 | 392 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| held | s512 | 2 | 2048 | 1199 | 41.4 | 600 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| upstream | s288 | 2 | 2304 | 725 | 68.5 | 363 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| upstream | s384 | 2 | 3072 | 909 | 70.4 | 454 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| upstream | s320 | 2 | 1920 | 783 | 59.2 | 392 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
| upstream | s512 | 2 | 2048 | 1199 | 41.4 | 600 |  |  |  | 0+ | 2 | 2 | 0 | 0 | 40 |
