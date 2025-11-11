#!/usr/bin/env bash

FIELD="$1"
VALUE="$2"

if [ -z "$FIELD" ] || [ -z "$VALUE" ]; then
  echo "Error: FIELD and VALUE arguments are required"
  echo "Usage: $0 <field-name> <value>"
  exit 1
fi

PROJECT_NUMBER=2
OWNER="liouk"
PROJECT_ID=$(gh project view $PROJECT_NUMBER --owner $OWNER --format json | jq -r '.id')

FIELD_ID=$(gh project field-list $PROJECT_NUMBER --owner $OWNER --format json | jq -r '.fields[] | select(.name=="'"$FIELD"'") | .id')
NEW_VALUE_ID=$(gh project field-list $PROJECT_NUMBER --owner $OWNER --format json | jq -r '.fields[] | select(.name=="'"$FIELD"'") | .options[] | select(.name=="'"$VALUE"'") | .id')

# Get all item IDs you want to move (filter however you like)
FIELD_LOWER=$(echo "$FIELD" | tr '[:upper:]' '[:lower:]')
ITEM_IDS=$(gh project item-list $PROJECT_NUMBER --owner $OWNER --format json --limit 1000 |
           jq -r --arg field "$FIELD_LOWER" '.items[] | select(.[$field] == null) | .id')

# Count items and confirm
ITEM_COUNT=$(echo "$ITEM_IDS" | wc -w)
echo "Found $ITEM_COUNT items to update"
echo "Setting field '$FIELD' to '$VALUE' for all items"
read -p "Continue? (y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "Abort."
  exit 0
fi

# Loop and update each
for ITEM_ID in $ITEM_IDS; do
  gh project item-edit \
  	--project-id "$PROJECT_ID" \
    --id "$ITEM_ID" \
    --field-id "$FIELD_ID" \
    --single-select-option-id "$NEW_VALUE_ID"
done