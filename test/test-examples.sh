#!/bin/bash
# check that our examples still build, even if we don't run them here

# shellcheck disable=SC1091
. test/util.sh

echo running "$0"

ROOT=$(dirname "${BASH_SOURCE}")/..
cd "${ROOT}"

failures=''

# TODO: test examples/lang/ directory to see if the .mcl files compile correctly

buildout='test-examples.out'

trap 'rm -fv "$buildout"' EXIT # clean up build mess

# loop through individual *.go files in working dir
find examples/lib -maxdepth 9 -type f -name '*.go' | while read -r file; do
	echo "running test on: $file"
	run-test go build -o "$buildout" "$file" || fail_test "could not build: $file"
done

if [[ -n "$failures" ]]; then
	echo 'FAIL'
	echo "The following tests have failed:"
	echo -e "$failures"
	echo
	exit 1
fi
echo 'PASS'
