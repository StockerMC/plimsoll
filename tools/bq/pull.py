"""Download the extract tables as sorted gzip CSVs through the BigQuery Storage Read API."""

import os
import sys

from google.cloud import bigquery

PROJECT = "vpa-ensemble"
DATASET = "borg"
SORT = {
    "collections": ["collection_id"],
    "sample": ["collection_id"],
    "events": ["collection_id", "instance_index", "time", "type"],
    "usage": ["collection_id", "instance_index", "start_time"],
}


def main():
    if len(sys.argv) < 2:
        sys.exit("usage: pull.py CELL [OUT_DIR [TABLE...]]")
    cell = sys.argv[1]
    out = sys.argv[2] if len(sys.argv) > 2 else f"data/cell-{cell}"
    tables = sys.argv[3:] or list(SORT)
    os.makedirs(out, exist_ok=True)
    client = bigquery.Client(project=PROJECT)
    for base in tables:
        name = f"{cell}_{base}"
        table = client.get_table(f"{PROJECT}.{DATASET}.{name}")
        df = client.list_rows(table).to_dataframe(create_bqstorage_client=True)
        df = df.sort_values(SORT[base], kind="stable")
        path = os.path.join(out, f"{name}.csv.gz")
        df.to_csv(path, index=False, compression="gzip")
        print(f"{name}: {len(df)} rows (table {table.num_rows}) -> {path} {os.path.getsize(path)} bytes")
        if len(df) != table.num_rows:
            sys.exit(f"{name}: row count mismatch")


if __name__ == "__main__":
    main()
