#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 4 || ! "$2" =~ ^com\.unity\.pipeline-[a-zA-Z0-9.-]+\.tgz$ ||
      ! "$3" =~ ^[[:xdigit:]]{64}$ || ! "$4" =~ ^[[:xdigit:]]{64}$ ]]; then
    echo 'Usage: verify-release-assets.sh <directory> <UPM filename> <Windows SHA256> <UPM SHA256>' >&2
    exit 2
fi

dir=$1
package=$2
windows_sha=${3,,}
upm_sha=${4,,}
cd "$dir"

artifacts=(upipe.exe upipe-linux-amd64 upipe-darwin-amd64 upipe-darwin-arm64 "$package")
shopt -s dotglob nullglob
files=(*)
if [[ ${#files[@]} != 11 ]]; then
    echo 'Expected five artifacts, five individual checksums and SHA256SUMS.' >&2
    exit 1
fi

manifest=()
for file in "${artifacts[@]}"; do
    hash_line=$(sha256sum --binary "$file")
    digest=${hash_line%% *}
    if [[ ( "$file" == upipe.exe && "$digest" != "$windows_sha" ) ||
          ( "$file" == "$package" && "$digest" != "$upm_sha" ) ]]; then
        echo "$file differs from the acceptance-tested artifact." >&2
        exit 1
    fi
    actual="$digest  $file"
    if [[ "$(tr -d '\r' < "$file.sha256")" != "$actual" ]]; then
        echo "$file has an inconsistent .sha256 file." >&2
        exit 1
    fi
    manifest+=("$actual")
done
diff <(printf '%s\n' "${manifest[@]}" | LC_ALL=C sort) <(tr -d '\r' < SHA256SUMS | LC_ALL=C sort)
