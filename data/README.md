# data

Not committed except this file. `data/cell-a/` holds the cell a extract of the Borg 2019 trace (ClusterData2019, CC-BY 4.0).

## Pull

Pulled 2026-10-28 into BigQuery project `vpa-ensemble` (sandbox, no billing account) with `scripts/bq-extract.sh`, which runs `tools/bq/q1_collections.sql` to `q4_usage.sql` against `google.com:google-cluster-data.clusterdata_2019_a`, each dry-run first and billed under a byte cap. Tables written to dataset `vpa-ensemble:borg` (60-day expiration). Downloaded with `tools/bq/pull.py` through the BigQuery Storage Read API, sorted, gzip CSV.

| query | cap (bytes) | dry run (bytes) | billed (bytes) | result rows |
|---|---|---|---|---|
| q1_collections | 4e9 | 3,083,411,868 | 3,083,862,016 | 1,817 |
| q2_events | 8e10 | 68,692,347,952 | 68,693,262,336 | 10,797,248 |
| q3_sample | 2e10 | 432,115,132 | 433,061,888 | 400 |
| q3_sample, rerun with `type` breaking `time` ties | 2e10 | 432,115,132 | 433,061,888 | 400 |
| q1 audit: population collections with a termination before the end sentinel | 1e9 | 499,410,856 | 500,170,752 | 0 |
| q4_usage | 4.5e11 | 423,885,513,512 | 423,885,799,424 | 24,036,193 |

Total billed about 0.452 TiB of the 1 TiB monthly free tier. `instance_usage` is billed for the whole table's columns regardless of the WHERE clause: do not rerun q4 this month.

## Population and sample

- `a_collections`: jobs alive the whole month (event at `time = 0`, no EVICT/FAIL/FINISH/KILL/LOST before the end sentinel), top level (`collection_type = 0`, no alloc), priority 120 to 359, `scheduling_class >= 2`, `vertical_scaling` 1 (OFF) or 3 (FULLY_AUTOMATED). 633 OFF, 1,184 FULLY_AUTOMATED.
- `a_sample`: collections with 2 to 50 instances, the first 200 per `vertical_scaling` ordered by `FARM_FINGERPRINT(CAST(collection_id AS STRING))`. Deterministic, no seed.
- `a_events`: every `instance_events` row of the 1,817 population collections (not only the sample).
- `a_usage`: every `instance_usage` row of the 400 sample collections.

## Files

| file | rows | gzip bytes | columns |
|---|---|---|---|
| a_collections.csv.gz | 1,817 | 92,242 | collection_id, priority, scheduling_class, vertical_scaling (int), user, logical_name (string) |
| a_sample.csv.gz | 400 | 25,997 | a_collections columns, n_instances, n_update_running, n_mem_changes (int) |
| a_events.csv.gz | 10,797,248 | 59,153,924 | collection_id, instance_index, time, type (int), req_mem (float) |
| a_usage.csv.gz | 24,036,193 | 209,716,315 | collection_id, instance_index, start_time, end_time (int), avg_mem, max_mem, assigned_memory (float) |

Sorted by `collection_id`, then `instance_index` and `time`/`start_time`. Times are microseconds since 600 s before the trace start; memory is a fraction of the largest machine (never bytes).

## check_extract.py

```
sample collections by vertical_scaling: {1: 200, 3: 200}
sample instances by vertical_scaling: {1: 1334, 3: 1170}
sample collections with a memory change by vertical_scaling: {1: 6, 3: 199}
sample collections with no usage rows: 1
usage collections: 399
events collections: 1817 of which in sample: 400
max_mem null: 0 (0.000000%), zero: 16106 (0.067007%)
assigned_memory zero or null: 0.055254%
rows under 300 s: 10.3017%; over 300 s: 0
start_time min/max: 300000000 2678999000000 (0.003 to 31.007 days)
end_time max: 2679000000000 sentinel rows: 0
rows with max_mem > assigned_memory: 0.1312%
instance-windows (collection, instance, 300 s bucket): 21908462
collection-windows (collection, 300 s bucket): 3562635
collection-days (collection, trace day of start_time): 12369
```

Notes: collection 218403970713 (FULLY_AUTOMATED, 3 instances) is in the sample but has no usage rows, so 399 collections replay. A memory change (`n_mem_changes`, and the fixture counts) is a type 10 event whose `req_mem` differs from the instance's previous event, with events at the same `time` ordered by `type`. The q1 audit row shows the 1,817 collections have no termination before the end sentinel, so the stricter q1 filter selects the same population the first run built. Six OFF collections show memory request changes in `instance_events` (15671199, 2394450791, 15056822994, 77653926668, 97504723629, 199502247421), so OFF is not a change-free control without filtering.

## Tune and report split

Within cell a. A collection goes to `tune` or `report` by the low bit of FNV-1a over `user + "/" + logical_name` (over the decimal `collection_id` when either is empty), so collections of one logical job land on the same side. The same key is the bootstrap's resampling unit.
