// Command replay runs recommenders over the Borg 2019 extract in data/ and
// writes per-run summaries under results/. It is open loop: a limit below an
// observed peak counts as a limit-exceedance window, nothing feeds back.
package main
