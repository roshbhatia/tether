#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 || $# -gt 3 ]]; then
  echo "usage: ./hack/init-template.sh OWNER PROJECT [BINARY]" >&2
  exit 2
fi

owner=$1
project=$2
binary=${3:-$project}
if [[ ${#owner} -gt 39 || ! $owner =~ ^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$ ]]; then
  echo "ERROR: OWNER must be a valid GitHub account name" >&2
  exit 2
fi
for value in "$project" "$binary"; do
  if [[ ! $value =~ ^[a-z][a-z0-9-]*$ ]]; then
    echo "ERROR: PROJECT and BINARY must start with a lowercase letter and contain only lowercase letters, digits, and hyphens" >&2
    exit 2
  fi
done

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if ! grep -q '"initialized": false' template.json; then
  echo "ERROR: this template has already been initialized" >&2
  exit 1
fi

git grep -Ilz \
  -e 'github.com/roshbhatia/go-cli-template' \
  -e 'go-cli-template' \
  -e 'example' \
  -- . ':!hack/init-template.sh' |
  xargs -0 perl -pi -e \
    's{github\.com/roshbhatia/go-cli-template}{__GO_CLI_TEMPLATE_MODULE__}g; s{go-cli-template}{__GO_CLI_TEMPLATE_PROJECT__}g; s{example}{__GO_CLI_TEMPLATE_BINARY__}g; s{owner: roshbhatia}{owner: __GO_CLI_TEMPLATE_OWNER__}g; s{"owner": "roshbhatia"}{"owner": "__GO_CLI_TEMPLATE_OWNER__"}g'

git grep -Ilz \
  -e '__GO_CLI_TEMPLATE_' \
  -- . ':!hack/init-template.sh' |
  xargs -0 perl -pi -e \
    's{__GO_CLI_TEMPLATE_MODULE__}{github.com/'"$owner"'/'"$project"'}g; s{__GO_CLI_TEMPLATE_PROJECT__}{'"$project"'}g; s{__GO_CLI_TEMPLATE_BINARY__}{'"$binary"'}g; s{__GO_CLI_TEMPLATE_OWNER__}{'"$owner"'}g'

perl -pi -e 's{"initialized": false}{"initialized": true}' template.json

if [[ $binary != example ]]; then
  git mv completions/example.bash "completions/${binary}.bash"
  git mv completions/example.fish "completions/${binary}.fish"
  git mv completions/example.nu "completions/${binary}.nu"
  git mv completions/_example "completions/_${binary}"
fi

go mod tidy
"$BASH" ./hack/generate.sh
