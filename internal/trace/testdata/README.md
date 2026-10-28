# Trace fixtures

Three collections from the cell a extract (`data/README.md`), cut by `tools/bq/cut_fixtures.py`: rows with `time` or `start_time` before 600 s + 48 h. Same columns as the extract files.

| collection_id | vertical_scaling | instances | why |
|---|---|---|---|
| 11740794268 | 3 (FULLY_AUTOMATED) | 2 | 17 memory request changes (type 10 whose `req_mem` differs from the instance's previous event, ties on `time` ordered by `type`); both instances scheduled more than once |
| 32345 | 1 (OFF) | 2 | no memory changes, no restarts, all windows 300 s |
| 41324957395 | 1 (OFF) | 2 | both instances restart under the same index with no request change; 296 windows under 300 s; one row with `max_mem` 0 (instance 0, start_time 118796000000) and one with `max_mem` empty (instance 0, start_time 300000000) |

The zero `max_mem` row is from the trace. The empty one was blanked by the cut script, since the extract has no null `max_mem`.
