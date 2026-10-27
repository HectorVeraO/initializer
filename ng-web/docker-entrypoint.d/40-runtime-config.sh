#!/bin/sh
# Generates the SPA runtime config from the WEB_* variables before nginx starts.
set -eu

# Escapes backslashes and double quotes so a value cannot break the JSON.
json_escape() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

TARGET=/usr/share/nginx/html/assets/config/runtime-config.json
mkdir -p "$(dirname "$TARGET")"

cat > "$TARGET" <<JSON
{
  "apiUrl": "$(json_escape "${WEB_API_URL:-}")"
}
JSON
