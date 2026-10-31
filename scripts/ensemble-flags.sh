#!/bin/sh
# Usage: ensemble-flags.sh CONFIG NAME
# Prints the recommender flags for the ensemble entry NAME in CONFIG (ensemble-held or ensemble-ratio).
set -eu
# The live binary exposes only these parameters; the rest must equal its built-in values.
jq -er --arg n "$2" '.[$n] |
	if (.decay | IN(0, 0.2)) and .underrun == 1 and .model_change == 0.1
	then "--ensemble-overrun-weight=\(.overrun) --ensemble-limit-change-weight=\(.limit_change) --ensemble-fall-fraction=\(.fall_fraction) --ensemble-fall-windows=\(.fall_windows)" +
		(if (.ratio_windows // 0) > 0 then " --ensemble-ratio-windows=\(.ratio_windows) --ensemble-ratio-floor=\(.ratio_floor)" else "" end)
	else error("\($n) sets a parameter the recommender has no flag for") end' "$1"
