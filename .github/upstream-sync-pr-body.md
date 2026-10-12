Automated weekly sync with microsoft/TypeScript's Go compiler (`tsc/`).

`scripts/sync-upstream.sh` replaced every upstream file with a snapshot of `tsc/` at the commit in `scripts/upstream-ref`, deleted the upstream files the snapshot no longer has, and re-ran `scripts/rename-module.sh`. It never touches a path in `scripts/owned-paths`, and `go build ./...` passed before it committed.

That is correct only while the golden rule holds, so CI runs `scripts/check-upstream-snapshot.sh` on this PR: every upstream file must be byte-identical to the snapshot after the module rename.

**Review checklist**
- [ ] `scripts/check-upstream-snapshot.sh` passes
- [ ] cakebear's own tests still pass against the new frontend
- [ ] No new upstream API that `lower/` or `cmd/cakec` should adopt
