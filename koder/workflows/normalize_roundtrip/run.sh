#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
CASES_FILE="$SCRIPT_DIR/cases.txt"
RESULTS_DIR="$SCRIPT_DIR/results"
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

NUM_RUNS=1
if [[ "${1:-}" == "--runs" ]]; then
    NUM_RUNS="${2:-1}"
elif [[ -n "${1:-}" ]]; then
    NUM_RUNS="$1"
fi

mkdir -p "$RESULTS_DIR"

TIMESTAMP=$(date +%Y%m%dT%H%M%S)
RESULT_FILE="$RESULTS_DIR/run_${TIMESTAMP}.txt"

# Parse cases from cases.txt
parse_cases() {
    local input="" expect=""
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ "$line" =~ ^#.*$ ]] && continue

        if [[ "$line" =~ ^INPUT:\ (.+)$ ]]; then
            input="${BASH_REMATCH[1]}"
        elif [[ "$line" =~ ^EXPECT:\ (.+)$ ]]; then
            expect="${BASH_REMATCH[1]}"
            if [[ -n "$input" && -n "$expect" ]]; then
                echo "CASE_INPUT:$input"
                echo "CASE_EXPECT:$expect"
            fi
            input="" expect=""
        fi
    done < "$CASES_FILE"
}

# Word overlap similarity (case-insensitive, punctuation-stripped)
# Returns percentage of expected words found in got
similarity() {
    local expected="$1" got="$2"

    local -a exp_words got_words
    read -ra exp_words <<< "$(echo "$expected" | tr '[:upper:]' '[:lower:]' | sed "s/[^a-z0-9.' ]/ /g" | tr -s ' ')"
    read -ra got_words <<< "$(echo "$got" | tr '[:upper:]' '[:lower:]' | sed "s/[^a-z0-9.' ]/ /g" | tr -s ' ')"

    if [[ ${#exp_words[@]} -eq 0 ]]; then
        echo "0"
        return
    fi

    local matches=0
    local -A got_map
    for w in "${got_words[@]}"; do
        got_map["$w"]=$(( ${got_map["$w"]:-0} + 1 ))
    done

    for w in "${exp_words[@]}"; do
        if [[ ${got_map["$w"]:-0} -gt 0 ]]; then
            matches=$((matches + 1))
            got_map["$w"]=$(( ${got_map["$w"]} - 1 ))
        fi
    done

    echo $(( matches * 100 / ${#exp_words[@]} ))
}

# Build cattery once
echo "Building cattery..."
(cd "$REPO_ROOT" && go build -o "$TMP_DIR/cattery" ./cmd/cattery)
CATTERY="$TMP_DIR/cattery"

echo "Normalize round-trip test"
echo "Runs: $NUM_RUNS"
echo "Results: $RESULT_FILE"
echo ""

# Collect cases
mapfile -t case_lines < <(parse_cases)

total_score=0
total_cases=0

for run in $(seq 1 "$NUM_RUNS"); do
    echo "=== Run $run/$NUM_RUNS at $(date -Iseconds) ===" | tee -a "$RESULT_FILE"
    echo "" >> "$RESULT_FILE"

    input="" expect=""
    for cl in "${case_lines[@]}"; do
        if [[ "$cl" =~ ^CASE_INPUT:(.+)$ ]]; then
            input="${BASH_REMATCH[1]}"
        elif [[ "$cl" =~ ^CASE_EXPECT:(.+)$ ]]; then
            expect="${BASH_REMATCH[1]}"

            wav_file="$TMP_DIR/test_${total_cases}.wav"

            # TTS → WAV
            $CATTERY speak -o "$wav_file" "$input" 2>/dev/null || true

            # WAV → STT
            got=$($CATTERY listen "$wav_file" 2>/dev/null || echo "[STT FAILED]")
            got=$(echo "$got" | tr -d '\n' | sed 's/^ *//;s/ *$//')

            # Score: how much of the expected output did STT recover?
            score=$(similarity "$expect" "$got")
            total_score=$((total_score + score))
            total_cases=$((total_cases + 1))

            {
                echo "CASE:     $input"
                echo "EXPECTED: $expect"
                echo "GOT:      $got"
                echo "SCORE:    ${score}%"
                echo ""
            } | tee -a "$RESULT_FILE"

            input="" expect=""
        fi
    done

    echo "---" | tee -a "$RESULT_FILE"
    echo "" >> "$RESULT_FILE"
done

if [[ $total_cases -gt 0 ]]; then
    avg=$((total_score / total_cases))
    {
        echo "=== Summary ==="
        echo "Total cases: $total_cases"
        echo "Average score: ${avg}%"
    } | tee -a "$RESULT_FILE"
else
    echo "No cases found in $CASES_FILE" | tee -a "$RESULT_FILE"
fi
