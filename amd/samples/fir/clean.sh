#!/bin/bash
# Delete akita_sim_* temp files in this workload's subtree.
# Refuses if any binary for this workload is running (to avoid the
# "unlink while open → permanent data loss" problem).
set -euo pipefail

WORKLOAD_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
WORKLOAD_NAME=$(basename "$WORKLOAD_DIR")

# Refuse if the workload binary is currently running anywhere
running=$(pgrep -af "/$WORKLOAD_NAME/$WORKLOAD_NAME" 2>/dev/null || true)
if [ -n "$running" ]; then
    echo "ABORT: $WORKLOAD_NAME binary is running:"
    echo "$running"
    echo
    echo "Stop the simulation before cleaning, or akita_sim_*.sqlite3 will be"
    echo "unlinked while still open by the process — data writes after the unlink"
    echo "go to a deleted inode and are permanently lost at process exit."
    exit 1
fi

# Find temp files (sqlite + variants), including the literal '(deleted)' suffix
# that appears if a prior recovery used readlink output of a deleted inode.
mapfile -t FILES < <(find "$WORKLOAD_DIR" \
    \( -name 'akita_sim_*.sqlite3' \
    -o -name 'akita_sim_*.sqlite3-wal' \
    -o -name 'akita_sim_*.sqlite3-shm' \
    -o -name 'akita_sim_*.sqlite3-journal' \
    -o -name 'akita_sim_*.sqlite3 (deleted)' \
    \) -type f 2>/dev/null)

if [ "${#FILES[@]}" -eq 0 ]; then
    echo "No akita_sim_* temp files in $WORKLOAD_DIR — nothing to clean."
    exit 0
fi

echo "Deleting ${#FILES[@]} temp file(s) under $WORKLOAD_DIR:"
for f in "${FILES[@]}"; do
    rm -v -- "$f"
done
