#!/bin/sh
# Compile every Blueprint file (*.blp) in the given directories to GtkBuilder
# XML (*.ui) next to it. The .ui files are committed, so only people who edit
# .blp files need blueprint-compiler (sudo apt install blueprint-compiler).
# BLUEPRINT overrides the compiler command.
set -eu
BLUEPRINT=${BLUEPRINT:-blueprint-compiler}
for dir in "$@"; do
	for blp in "$dir"/*.blp; do
		out=${blp%.blp}.ui
		$BLUEPRINT compile --output "$out" "$blp"
		# Some blueprint-compiler releases omit the final newline.
		python3 -c 'from pathlib import Path; import sys; p = Path(sys.argv[1]); b = p.read_bytes(); p.write_bytes(b if b.endswith(b"\n") else b + b"\n")' "$out"
	done
done
