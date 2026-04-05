#!/usr/bin/env bash
set -euo pipefail

# Get the latest tag
latest=$(git tag -l 'v*' --sort=-v:refname | head -1)
if [ -z "$latest" ]; then
  echo "No existing tags found. Starting from v0.0.0."
  latest="v0.0.0"
fi

# Parse semver
major=$(echo "$latest" | sed 's/v//' | cut -d. -f1)
minor=$(echo "$latest" | sed 's/v//' | cut -d. -f2)
patch=$(echo "$latest" | sed 's/v//' | cut -d. -f3)

echo "Current version: $latest"
echo ""
echo "  1) patch  → v${major}.${minor}.$((patch + 1))"
echo "  2) minor  → v${major}.$((minor + 1)).0"
echo "  3) major  → v$((major + 1)).0.0"
echo ""
read -rp "Bump type [1/2/3]: " choice

case "$choice" in
  1) new="v${major}.${minor}.$((patch + 1))" ;;
  2) new="v${major}.$((minor + 1)).0" ;;
  3) new="v$((major + 1)).0.0" ;;
  *) echo "Invalid choice"; exit 1 ;;
esac

echo ""
echo "Tagging $new"
echo ""

# Show what will be released
git log --oneline "${latest}..HEAD"
echo ""
read -rp "Proceed? [y/N]: " confirm
if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
  echo "Aborted."
  exit 0
fi

git tag "$new"
git push origin main "$new"

echo ""
echo "Released $new"
