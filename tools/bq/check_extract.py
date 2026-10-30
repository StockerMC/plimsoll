"""Print sanity counts for a pulled extract directory."""

import os
import sys

import pandas as pd

DAY = 86400 * 10**6
WINDOW = 300 * 10**6
END = 2**63 - 1
START = 600 * 10**6
DAYS = 31


def main():
    if len(sys.argv) < 2:
        sys.exit("usage: check_extract.py CELL [DIR]")
    cell = sys.argv[1]
    d = sys.argv[2] if len(sys.argv) > 2 else f"data/cell-{cell}"
    path = {n: os.path.join(d, f"{cell}_{n}.csv.gz") for n in ("collections", "sample", "events", "usage")}
    t = {n: pd.read_csv(p) for n, p in path.items()}
    for n, df in t.items():
        print(f"{cell}_{n}: {len(df)} rows, {os.path.getsize(path[n])} bytes gzip")

    s, u, e = t["sample"], t["usage"], t["events"]
    print("sample collections by vertical_scaling:", s.groupby("vertical_scaling").size().to_dict())
    print("sample instances by vertical_scaling:", s.groupby("vertical_scaling")["n_instances"].sum().to_dict())
    print("sample collections with a memory change by vertical_scaling:",
          s[s["n_mem_changes"] > 0].groupby("vertical_scaling").size().to_dict())

    missing = set(s["collection_id"]) - set(u["collection_id"])
    print("sample collections with no usage rows:", len(missing))
    print("usage collections:", u["collection_id"].nunique())
    print("events collections:", e["collection_id"].nunique(),
          "of which in sample:", e["collection_id"].isin(s["collection_id"]).groupby(e["collection_id"]).first().sum())

    mm = u["max_mem"]
    print(f"max_mem null: {mm.isna().sum()} ({mm.isna().mean():.6%}), zero: {(mm == 0).sum()} ({(mm == 0).mean():.6%})")
    print(f"assigned_memory zero or null: {((u['assigned_memory'] == 0) | u['assigned_memory'].isna()).mean():.6%}")
    dur = u["end_time"] - u["start_time"]
    print(f"rows under 300 s: {(dur < WINDOW).mean():.4%}; over 300 s: {(dur > WINDOW).sum()}")
    print("start_time min/max:", u["start_time"].min(), u["start_time"].max(),
          f"({u['start_time'].min() / DAY:.3f} to {u['start_time'].max() / DAY:.3f} days)")
    print("end_time max:", u["end_time"].max(), "sentinel rows:", (u["end_time"] == END).sum())
    print(f"rows with max_mem > assigned_memory: {(mm > u['assigned_memory']).mean():.4%}")

    bucket = u["start_time"] // WINDOW
    print("instance-windows (collection, instance, 300 s bucket):",
          len(pd.DataFrame({"c": u["collection_id"], "i": u["instance_index"], "b": bucket}).drop_duplicates()))
    print("collection-windows (collection, 300 s bucket):",
          len(pd.DataFrame({"c": u["collection_id"], "b": bucket}).drop_duplicates()))
    day = ((u["start_time"] - START) // DAY).clip(0, DAYS - 1)
    print("collection-days (collection, trace day of start_time):",
          len(pd.DataFrame({"c": u["collection_id"], "d": day}).drop_duplicates()))


if __name__ == "__main__":
    main()
