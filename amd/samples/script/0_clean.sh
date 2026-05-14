#!/usr/bin/env bash
# 0_clean.sh — wipe simulation result artifacts, with per-category prompts.
#
# Categories (each prompted independently unless --yes is passed):
#   1. baseline/ (created by 2_copy_benchmarks.sh)
#   2. SQLite databases (*.sqlite3, *.sqlite3-journal, *.sqlite3-wal,
#      *.sqlite3-shm) inside amd/samples/<workload>/<variant>/
#   3. motivation_*.csv outputs left in workload directories
#   4. results/<variant>/rawdata/{sql,text}/ contents (aggregated raw data)
#   5. results/per_window/<workload>/ and results/results/per_window/<workload>/
#   6. results/summary.csv
#
# Flags:
#   -y, --yes        skip all prompts (delete every category)
#   -n, --dry-run    list targets but do not delete
#   -h, --help       show this help
#
# Scripts (.py, .sh), the directory skeleton, and analysis docs under
# results/m1_analysis, results/ablation_planning, results/data,
# results/dnn_validation, results/sweep_log are intentionally NOT touched.

set -uo pipefail

ASSUME_YES=0
DRY_RUN=0
while [ $# -gt 0 ]; do
    case "$1" in
        -y|--yes) ASSUME_YES=1 ;;
        -n|--dry-run) DRY_RUN=1 ;;
        -h|--help)
            sed -n '2,18p' "$0"
            exit 0
            ;;
        *) echo "Unknown flag: $1" >&2; exit 2 ;;
    esac
    shift
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SAMPLES_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
RESULTS_DIR="$(cd "${SCRIPT_DIR}/../../../../results" 2>/dev/null && pwd || true)"

# confirm <prompt> — return 0 if user accepts, 1 otherwise.
# Honors --yes (always accept) and reads from /dev/tty so prompts work even
# when stdout is piped.
confirm() {
    if [ "$ASSUME_YES" -eq 1 ]; then
        echo "  [auto-yes] $1"
        return 0
    fi
    local reply
    if ! read -r -p "  $1 [y/N] " reply </dev/tty 2>/dev/null; then
        # No tty available — default to skip rather than guess.
        echo "  (no tty; skipping)"
        return 1
    fi
    case "$reply" in
        y|Y|yes|YES) return 0 ;;
        *) return 1 ;;
    esac
}

# show_targets <max-display> <list> — print first N targets and a count line.
show_targets() {
    local max=$1; shift
    local count=$#
    if [ "$count" -eq 0 ]; then
        echo "    (no matching files)"
        return 1
    fi
    local i=0
    for t in "$@"; do
        i=$((i + 1))
        [ "$i" -gt "$max" ] && break
        echo "    $t"
    done
    if [ "$count" -gt "$max" ]; then
        echo "    ... ($((count - max)) more)"
    fi
    echo "    [total: ${count}]"
    return 0
}

remove_files() {
    if [ "$DRY_RUN" -eq 1 ]; then
        echo "  (dry-run: would remove $# file(s))"
        return
    fi
    local removed=0
    for f in "$@"; do
        rm -f -- "$f" && removed=$((removed + 1))
    done
    echo "  removed ${removed} file(s)"
}

remove_dirs() {
    if [ "$DRY_RUN" -eq 1 ]; then
        echo "  (dry-run: would remove $# dir(s))"
        return
    fi
    local removed=0
    for d in "$@"; do
        rm -rf -- "$d" && removed=$((removed + 1))
    done
    echo "  removed ${removed} dir(s)"
}

clear_dir_contents() {
    # Remove everything inside the given dir but keep the dir itself.
    if [ "$DRY_RUN" -eq 1 ]; then
        echo "  (dry-run: would clear contents of $1)"
        return
    fi
    find "$1" -mindepth 1 -delete 2>/dev/null && echo "  cleared: $1"
}

echo "=== 0_clean.sh ==="
echo "samples: ${SAMPLES_DIR}"
echo "results: ${RESULTS_DIR:-<not found>}"
[ "$DRY_RUN" -eq 1 ] && echo "(dry-run mode — no files will be removed)"
echo ""

# --- Category 1: baseline/ directory -----------------------------------------
echo "[1] baseline/ directory (created by 2_copy_benchmarks.sh)"
baseline_dirs=()
[ -d "${SCRIPT_DIR}/baseline" ] && baseline_dirs+=("${SCRIPT_DIR}/baseline")
[ -d "${SAMPLES_DIR}/baseline" ] && baseline_dirs+=("${SAMPLES_DIR}/baseline")
if [ ${#baseline_dirs[@]} -gt 0 ]; then
    show_targets 10 "${baseline_dirs[@]}"
    if confirm "remove these baseline dir(s)?"; then
        remove_dirs "${baseline_dirs[@]}"
    else
        echo "  skipped"
    fi
else
    echo "  (none)"
fi
echo ""

# --- Category 2: SQLite artifacts under amd/samples --------------------------
echo "[2] SQLite databases under ${SAMPLES_DIR}"
mapfile -t sqlite_files < <(find "${SAMPLES_DIR}" -mindepth 2 -type f \
    \( -name "*.sqlite3" -o -name "*.sqlite3-journal" \
       -o -name "*.sqlite3-wal" -o -name "*.sqlite3-shm" \) \
    -not -path "*/script/*" 2>/dev/null)
if show_targets 10 "${sqlite_files[@]}"; then
    if confirm "delete these sqlite file(s)?"; then
        remove_files "${sqlite_files[@]}"
    else
        echo "  skipped"
    fi
fi
echo ""

# --- Category 3: motivation_*.csv outputs ------------------------------------
echo "[3] motivation_*.csv outputs in workload dirs"
mapfile -t csv_files < <(find "${SAMPLES_DIR}" -mindepth 2 -type f \
    -name "motivation_*.csv" -not -path "*/script/*" 2>/dev/null)
if show_targets 10 "${csv_files[@]}"; then
    if confirm "delete these csv file(s)?"; then
        remove_files "${csv_files[@]}"
    else
        echo "  skipped"
    fi
fi
echo ""

# --- Category 4: results/<variant>/rawdata/{sql,text} contents ---------------
echo "[4] aggregated raw data (results/<variant>/rawdata/{sql,text})"
rawdata_dirs=()
if [ -n "${RESULTS_DIR}" ] && [ -d "${RESULTS_DIR}" ]; then
    mapfile -t rawdata_dirs < <(find "${RESULTS_DIR}" -mindepth 3 -maxdepth 4 \
        -type d \( -name "sql" -o -name "text" \) 2>/dev/null)
fi
if [ ${#rawdata_dirs[@]} -gt 0 ]; then
    show_targets 20 "${rawdata_dirs[@]}"
    if confirm "clear contents of these rawdata dir(s)?"; then
        for d in "${rawdata_dirs[@]}"; do
            clear_dir_contents "$d"
        done
    else
        echo "  skipped"
    fi
else
    echo "  (none)"
fi
echo ""

# --- Category 5: per_window subdirs ------------------------------------------
echo "[5] per_window result dirs"
pw_dirs=()
if [ -n "${RESULTS_DIR}" ] && [ -d "${RESULTS_DIR}" ]; then
    for pw_root in "${RESULTS_DIR}/per_window" "${RESULTS_DIR}/results/per_window"; do
        [ -d "$pw_root" ] || continue
        while IFS= read -r d; do
            pw_dirs+=("$d")
        done < <(find "$pw_root" -mindepth 1 -maxdepth 1 -type d 2>/dev/null)
    done
fi
if [ ${#pw_dirs[@]} -gt 0 ]; then
    show_targets 20 "${pw_dirs[@]}"
    if confirm "remove these per_window subdir(s)?"; then
        remove_dirs "${pw_dirs[@]}"
    else
        echo "  skipped"
    fi
else
    echo "  (none)"
fi
echo ""

# --- Category 6: results/summary.csv -----------------------------------------
echo "[6] results/summary.csv"
if [ -n "${RESULTS_DIR}" ] && [ -f "${RESULTS_DIR}/summary.csv" ]; then
    show_targets 1 "${RESULTS_DIR}/summary.csv"
    if confirm "delete summary.csv?"; then
        remove_files "${RESULTS_DIR}/summary.csv"
    else
        echo "  skipped"
    fi
else
    echo "  (none)"
fi
echo ""

echo "Done."
