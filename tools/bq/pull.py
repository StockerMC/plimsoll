"""Download the extract tables as sorted gzip CSVs through the BigQuery Storage Read API."""

import os
import sys

from google.cloud import bigquery

PROJECT = "vpa-ensemble"
DATASET = "borg"
SORT = {
    "a_collections": ["collection_id"],
    "a_sample": ["collection_id"],
    "a_events": ["collection_id", "instance_index", "time", "type"],
    "a_usage": ["collection_id", "instance_index", "start_time"],
}


def main():
    out = sys.argv[1] if len(sys.argv) > 1 else "data/cell-a"
    tables = sys.argv[2:] or list(SORT)
    os.makedirs(out, exist_ok=True)
    client = bigquery.Client(project=PROJECT)
    for name in tables:
        table = client.get_table(f"{PROJECT}.{DATASET}.{name}")
        df = client.list_rows(table).to_dataframe(create_bqstorage_client=True)
        df = df.sort_values(SORT[name], kind="stable")
        path = os.path.join(out, f"{name}.csv.gz")
        df.to_csv(path, index=False, compression="gzip")
        print(f"{name}: {len(df)} rows (table {table.num_rows}) -> {path} {os.path.getsize(path)} bytes")
        if len(df) != table.num_rows:
            sys.exit(f"{name}: row count mismatch")


if __name__ == "__main__":
    main()
