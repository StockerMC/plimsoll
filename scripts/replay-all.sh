#!/usr/bin/env bash
# Usage: replay-all.sh sweep|report|compare
# sweep tunes on the tune split and writes results/summary/sweep-cella.csv;
# the chosen rows are copied into configs/default.json by hand and committed
# before report runs on the report split.
set -euo pipefail
cd "$(dirname "$0")/.."

date=$(date +%F)
prefix=cella-$date
go build -o bin/replay ./cmd/replay

case ${1:?usage: replay-all.sh sweep|report|compare} in
sweep)
	bin/replay --sweep configs/sweep-cella.json --split tune --out results/summary/sweep-cella.csv
	;;
report)
	bin/replay --split report --params configs/default.json --run-prefix "$prefix" \
		--recommender percentile,percentile-tuned,window,ml,ensemble,ensemble-held,ensemble-ratio,assigned,naive \
		--actuator every,updater --raw "results/raw/$prefix" | tee "results/summary/$prefix-report.log"
	;;
compare)
	for act in every updater; do
		for pair in ensemble:percentile-tuned ensemble:percentile ensemble:ml ensemble:window ml:percentile-tuned window:percentile-tuned \
			ensemble-held:percentile-tuned ensemble-held:window ensemble-held:ensemble ensemble-held:ml \
			ensemble-ratio:percentile-tuned ensemble-ratio:window ensemble-ratio:ensemble ensemble-ratio:ml ensemble-ratio:ensemble-held; do
			bin/replay --compare "results/summary/$prefix-${pair%%:*}-$act" "results/summary/$prefix-${pair##*:}-$act"
		done
	done
	;;
*)
	echo "usage: replay-all.sh sweep|report|compare" >&2
	exit 1
	;;
esac
