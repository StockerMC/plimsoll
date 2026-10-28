#!/usr/bin/env bash
# Builds the cell a extract tables in BigQuery. Each query is dry-run first and billed under a byte cap.
set -euo pipefail

PROJECT=vpa-ensemble
DATASET=$PROJECT:borg
DIR=$(cd "$(dirname "$0")/../tools/bq" && pwd)

if ! bq show "$DATASET" >/dev/null 2>&1; then
  bq mk --dataset --default_table_expiration 5184000 "$DATASET"
fi

run() {
  local sql=$DIR/$1 cap=$2
  local est job billed
  est=$(bq query --project_id=$PROJECT --use_legacy_sql=false --dry_run --format=json <"$sql" |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["statistics"]["totalBytesProcessed"])')
  echo "$1 dry run: $est bytes (cap $cap)"
  if python3 -c "import sys; sys.exit(0 if $est <= $cap else 1)"; then :; else
    echo "$1: dry run over cap, stopping" >&2
    exit 1
  fi
  job=extract_${1%.sql}_$(date +%s)
  bq query --project_id=$PROJECT --use_legacy_sql=false --maximum_bytes_billed="$cap" --job_id="$job" --format=none <"$sql"
  billed=$(bq show --project_id=$PROJECT --format=json -j "$job" |
    python3 -c 'import json,sys; s=json.load(sys.stdin)["statistics"]; print(s.get("totalBytesProcessed"), s.get("query",{}).get("totalBytesBilled"))')
  echo "$1 job $job processed/billed: $billed"
}

case "${1:-all}" in
  all)
    run q1_collections.sql 4000000000
    run q2_events.sql 80000000000
    run q3_sample.sql 20000000000
    run q4_usage.sql 450000000000
    ;;
  *) run "$1" "${2:?usage: bq-extract.sh [all | QUERY.sql CAP]}" ;;
esac
