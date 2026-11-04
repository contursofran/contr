PR #289, head 23d4275. scripts/check.sh passes locally; CI pending.
Library copied into Contents/Resources by bundle.sh; `contr terminal-check` added; Bundle CI runs it on the packaged app. D93.
Deviation: kept go build (no mygo build). I did not check other open PRs for conflicts.
