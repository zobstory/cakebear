#!/usr/bin/env bash
# Fetch the @types/node the tests hold cakebear to, into <dir>.
#
#   scripts/fetch-types-node.sh <dir>
#   CAKEBEAR_TYPES_NODE=<dir> go test ./cmd/cakec/...
#
# A server project usually has @types/node, and a program with it is built
# differently (S1 plan D12, cmd/cakec/typesnode.go). cakebear's tests install
# nothing, and vendoring the two packages would put 2.7 MB of someone else's
# declarations in this repository's history, so CI fetches the pinned
# versions here and the tests take them from CAKEBEAR_TYPES_NODE. With it
# unset those tests skip, saying so.
#
# <dir> gets node_modules/@types/node and node_modules/undici-types, which
# @types/node's fetch types import. npm checks each tarball against the
# registry's integrity hash.
set -euo pipefail

# The versions, and nowhere else: cmd/cakec/typesnode_test.go reads these two
# lines to check the copy it is given.
TYPES_NODE=24.19.2
UNDICI_TYPES=7.24.6

dir=${1:?usage: fetch-types-node.sh <dir>}
mkdir -p "$dir/node_modules/@types/node" "$dir/node_modules/undici-types"
packs=$(mktemp -d)
trap 'rm -rf "$packs"' EXIT
(cd "$packs" && npm pack --silent "@types/node@$TYPES_NODE" "undici-types@$UNDICI_TYPES" >/dev/null)
tar -xzf "$packs/types-node-$TYPES_NODE.tgz" -C "$dir/node_modules/@types/node" --strip-components=1
tar -xzf "$packs/undici-types-$UNDICI_TYPES.tgz" -C "$dir/node_modules/undici-types" --strip-components=1
echo "fetched @types/node $TYPES_NODE and undici-types $UNDICI_TYPES into $dir"
