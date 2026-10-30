# Cell e confirmation protocol

Fixed 2026-10-30T17:02:14Z, before any cell e query. Cell a was used for tuning and, informally, for choosing which variants to keep, so it is exploratory. Cell e is the confirmation.

Code afd98f3 (replay semantics unchanged since 9b449f8, which reproduces every cella-2026-10-30 ensemble, ensemble-held, ensemble-ratio and percentile-tuned collections.csv byte for byte). Config `configs/default.json` sha256 29468315096f8f59547746d04e94e76bfae719bd5884cfd21dd822eaaca8e39c, frozen at c0a0ed9. No sweep or retuning on cell e.

Extract: `CELL=e scripts/bq-extract.sh` with the cell a queries, only the cell letter in source and destination names changed. Rendered cell e SQL sha256: q1 86a17defb701bd492c2d608ec90817752f8f5ac538f9cfd945c6e0f8de7b82de, q2 d3f5f013757df143359016632d0c94350bebe2516b385f6fec352645d44287ce, q3 7efbce00f6ea755c20078c0f64940f69c3fb81de61f27d64d524e091d8be89d6, q4 da5d44a6839d9c39923093d01b2450354ae6679f491a34631246cdf899f4dc53. Same population predicates, 2 to 50 instances, first 200 per vertical_scaling by FARM_FINGERPRINT.

Replay: `RUN=celle-2026-10-30 CELL=e scripts/replay-all.sh report`, then `compare`. All sampled collections (`--split all`), same 48 h warm-up and next-trace-day scoring.

Primary: ensemble-ratio vs percentile-tuned, actuator every, paired over collections, 95% bootstrap CI over user/logical-name groups (2,000 resamples, seed 1).

PASS iff both:

1. the upper end of the paired slack difference CI (ensemble-ratio minus percentile-tuned) is below 0;
2. ensemble-ratio's limit-exceedance windows per collection-day are at most percentile-tuned's (ratio <= 1.0; if the baseline is 0 the candidate must be 0).

Anything else, including a refused or mismatched comparison, is NOT CONFIRMED.

Reported either way, not gates: memory-time change, limit-change ratio, win fraction, collection, group, day and window counts, user/logical-name overlap with cell a, the updater pair, and every other frozen recommender.
