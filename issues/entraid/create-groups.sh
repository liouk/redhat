#!/bin/bash
set -euo pipefail

CSV_FILE="${1:-groups.csv}"

if [[ ! -f "$CSV_FILE" ]]; then
  echo "Error: $CSV_FILE not found"
  exit 1
fi

tail -n +2 "$CSV_FILE" | while IFS=',' read -r name nickname description; do
  echo "Creating group: $name"
  az ad group create \
    --display-name "$name" \
    --mail-nickname "$nickname" \
    --description "$description"
done

echo "Done. Created $(tail -n +2 "$CSV_FILE" | wc -l) groups."
