#!/bin/sh
# Usage: ensemble-flags.sh CONFIG NAME [SECONDS]
# Prints the recommender flags for the ensemble entry NAME in CONFIG (ensemble-held or ensemble-ratio).
# SECONDS is how long one of the config's windows lasts live (default 60, one live memory window);
# its fall and ratio windows are rescaled to 1-minute live windows.
set -eu
# The live binary exposes only these parameters; the rest must equal its built-in values.
jq -er --arg n "$2" --argjson s "${3:-60}" '.[$n] |
	(.fall_windows * $s / 60) as $fall |
	((.ratio_windows // 0) * $s / 60) as $ratio |
	if (.decay | IN(0, 0.2)) and .underrun == 1 and .model_change == 0.1 and $fall == ($fall | floor) and $ratio == ($ratio | floor)
	then "--ensemble-overrun-weight=\(.overrun) --ensemble-limit-change-weight=\(.limit_change) --ensemble-fall-fraction=\(.fall_fraction) --ensemble-fall-windows=\($fall)" +
		(if $ratio > 0 then " --ensemble-ratio-windows=\($ratio) --ensemble-ratio-floor=\(.ratio_floor)" else "" end)
	else error("\($n) sets a parameter the recommender has no flag for, or its windows do not rescale to whole minutes") end' "$1"
