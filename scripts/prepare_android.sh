#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
generated_main="$project_root/src-tauri/gen/android/app/src/main/java/app/vela/mail/MainActivity.kt"

if [[ ! -d "$(dirname "$generated_main")" ]]; then
  echo "Android project is not initialized. Run: pnpm tauri android init" >&2
  exit 1
fi

cp "$project_root/src-tauri/android/MainActivity.kt" "$generated_main"
cd "$project_root"
pnpm tauri icon src-tauri/icons/icon.png
