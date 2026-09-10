Automated weekly sync with `microsoft/typescript-go`.

`scripts/sync-upstream.sh` merged `upstream/main`, took upstream's side of every
conflicted file, and re-ran `scripts/rename-module.sh`. That resolution is only
correct while the golden rule holds — if this PR shows changes to a file
cakebear owns, the script would have stopped instead, so review the diff for
anything outside upstream's tree.

`go build ./...` passed before the merge was committed.

**Review checklist**
- [ ] `git show --stat` shows only upstream paths plus module-path lines
- [ ] cakebear's own tests still pass against the new frontend
- [ ] No new upstream package that `cmd/cakec` should be adopting
