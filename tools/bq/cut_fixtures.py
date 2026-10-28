"""Cut three small collections, first 48 h each, from the extract into test fixtures."""

import os
import sys

import pandas as pd

CUTOFF = (600 + 48 * 3600) * 10**6
WINDOW = 300 * 10**6


def mem_changes(e):
    e = e.sort_values(["collection_id", "instance_index", "time", "type"], kind="stable")
    prev = e.groupby(["collection_id", "instance_index"])["req_mem"].shift()
    changed = (e["type"] == 10) & prev.notna() & (e["req_mem"] != prev)
    return changed.groupby(e["collection_id"]).sum()


def restarts(e):
    sched = e[e["type"] == 3].groupby(["collection_id", "instance_index"]).size()
    return (sched > 1).groupby(level=0).sum()


def main():
    src = sys.argv[1] if len(sys.argv) > 1 else "data/cell-a"
    out = sys.argv[2] if len(sys.argv) > 2 else "internal/trace/testdata"
    s = pd.read_csv(os.path.join(src, "a_sample.csv.gz"))
    c = pd.read_csv(os.path.join(src, "a_collections.csv.gz"))
    e = pd.read_csv(os.path.join(src, "a_events.csv.gz"))
    u = pd.read_csv(os.path.join(src, "a_usage.csv.gz"))

    e = e[e["collection_id"].isin(s["collection_id"]) & (e["time"] < CUTOFF)]
    u = u[u["collection_id"].isin(s["collection_id"]) & (u["start_time"] < CUTOFF)]
    s = s.set_index("collection_id")
    s["changes48"] = mem_changes(e).reindex(s.index, fill_value=0)
    s["restarts48"] = restarts(e).reindex(s.index, fill_value=0)
    s["short48"] = (u["end_time"] - u["start_time"] < WINDOW).groupby(u["collection_id"]).sum().reindex(s.index, fill_value=0)
    s["zero48"] = (u["max_mem"].isna() | (u["max_mem"] == 0)).groupby(u["collection_id"]).sum().reindex(s.index, fill_value=0)
    s = s[s.index.isin(u["collection_id"])].sort_index().sort_values("n_instances", kind="stable")

    fa = s[(s["vertical_scaling"] == 3) & (s["changes48"] >= 2)]
    fa = fa[fa["restarts48"] > 0] if (fa["restarts48"] > 0).any() else fa
    off = s[(s["vertical_scaling"] == 1) & (s["changes48"] == 0)]
    picked = [fa.index[0], off.index[0]]
    odd = s[(s["short48"] > 0) & (s["zero48"] > 0) & ~s.index.isin(picked)]
    rest = s[(s["short48"] > 0) & ~s.index.isin(picked)]
    picked.append((odd if len(odd) else rest).index[0])

    u = u[u["collection_id"].isin(picked)].sort_values(["collection_id", "instance_index", "start_time"], kind="stable")
    blanked = None
    third = u["collection_id"] == picked[2]
    if not u.loc[third, "max_mem"].isna().any():
        i = u[third & (u["max_mem"] > 0)].index[0]
        u.loc[i, "max_mem"] = float("nan")
        blanked = (int(picked[2]), int(u.loc[i, "instance_index"]), int(u.loc[i, "start_time"]))

    os.makedirs(out, exist_ok=True)
    frames = {
        "collections": c[c["collection_id"].isin(picked)].sort_values("collection_id"),
        "sample": s.loc[sorted(picked)].drop(columns=["changes48", "restarts48", "short48", "zero48"]).reset_index(),
        "events": e[e["collection_id"].isin(picked)].sort_values(["collection_id", "instance_index", "time", "type"], kind="stable"),
        "usage": u,
    }
    for name, df in frames.items():
        path = os.path.join(out, f"{name}.csv.gz")
        df.to_csv(path, index=False, compression="gzip")
        print(f"{name}: {len(df)} rows, {os.path.getsize(path)} bytes")
    for cid in picked:
        r = s.loc[cid]
        print(f"{cid}: vertical_scaling {r['vertical_scaling']}, {r['n_instances']} instances, "
              f"{r['changes48']} memory changes, {r['restarts48']} restarted instances, "
              f"{r['short48']} short windows, {r['zero48']} zero max_mem in 48 h")
    if blanked:
        print("blanked max_mem by hand at collection, instance, start_time:", blanked)


if __name__ == "__main__":
    main()
