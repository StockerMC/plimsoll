#!/usr/bin/env bash
# Usage: RUN=cella-<unique-id> [CELL=a] replay-all.sh sweep|report|compare
# sweep tunes on the tune split of cell a and writes results/summary/sweep-$RUN.csv;
# the chosen rows are copied into configs/default.json by hand and committed
# before report runs on the report split. Any other cell is held out whole:
# report replays all of its collections with the frozen config.
set -euo pipefail
cd "$(dirname "$0")/.."

prefix=${RUN:?set RUN to an explicit unique run prefix}
case $prefix in
*[!a-zA-Z0-9_-]*) echo "RUN must contain only letters, digits, underscores or hyphens" >&2; exit 1 ;;
esac
cell=${CELL:-a}
case $cell in
a) split=report ;;
[b-h]) split=all ;;
*) echo "CELL must be one of a to h" >&2; exit 1 ;;
esac
extract=(--data "data/cell-$cell" --prefix "${cell}_" --cell "$cell")

build() {
	go build -o bin/replay ./cmd/replay
}

case ${1:?usage: replay-all.sh sweep|report|compare} in
sweep)
	[ "$cell" = a ] || { echo "sweep runs on cell a only" >&2; exit 1; }
	out=results/summary/sweep-$prefix.csv
	(set -C; : > "$out")
	build
	bin/replay --sweep configs/sweep-cella.json --split tune --out "$out"
	;;
report)
	if compgen -G "results/summary/$prefix-*" > /dev/null; then
		echo "report prefix already exists: $prefix" >&2
		exit 1
	fi
	mkdir -p results/raw
	mkdir "results/raw/$prefix"
	build
	bin/replay "${extract[@]}" --split "$split" --params configs/default.json --run-prefix "$prefix" \
		--recommender percentile,percentile-tuned,window,ml,ensemble,ensemble-held,ensemble-ratio,assigned,naive \
		--actuator every,updater --raw "results/raw/$prefix" | tee "results/summary/$prefix-report.log"
	;;
compare)
	out=results/summary/$prefix-comparisons
	mkdir "$out"
	build
	for act in every updater; do
		for pair in ensemble:percentile-tuned ensemble:percentile ensemble:ml ensemble:window ml:percentile-tuned window:percentile-tuned \
			ensemble-held:percentile-tuned ensemble-held:window ensemble-held:ensemble ensemble-held:ml \
			ensemble-ratio:percentile-tuned ensemble-ratio:window ensemble-ratio:ensemble ensemble-ratio:ml ensemble-ratio:ensemble-held; do
			bin/replay --out "$out" --compare "results/summary/$prefix-${pair%%:*}-$act" "results/summary/$prefix-${pair##*:}-$act"
		done
	done
	;;
*)
	echo "usage: replay-all.sh sweep|report|compare" >&2
	exit 1
	;;
esac
