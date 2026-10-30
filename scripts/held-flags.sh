#!/bin/sh
# Usage: held-flags.sh CONFIG [SECONDS]
# Prints the recommender flags for ensemble-held in CONFIG. SECONDS is how long one of the
# config's windows lasts live (default 60, one live memory window); its fall windows are rescaled
# to 1-minute live windows.
set -eu
# The live binary exposes only these held parameters; the rest must equal its built-in values.
jq -er --argjson s "${2:-60}" '."ensemble-held" |
	(.fall_windows * $s / 60) as $fall |
	if (.decay | IN(0, 0.2)) and .underrun == 1 and .model_change == 0.1 and (.ratio_windows // 0) == 0 and $fall == ($fall | floor)
	then "--ensemble-overrun-weight=\(.overrun) --ensemble-limit-change-weight=\(.limit_change) --ensemble-fall-fraction=\(.fall_fraction) --ensemble-fall-windows=\($fall)"
	else error("ensemble-held sets a parameter the recommender has no flag for, or its fall windows do not rescale to whole minutes") end' "$1"
