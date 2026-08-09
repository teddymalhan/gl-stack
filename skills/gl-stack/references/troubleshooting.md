# Troubleshooting and recovery

## Rebase conflicts (exit 3)

`rebase` and `sync` both exit 3 on conflict. A failed `sync` restores every branch before returning;
a failed `rebase` stops with a resumable rebase state.

```bash
gl-stack rebase
# resolve the paths listed on stderr
git add <resolved-paths>
gl-stack rebase --continue     # repeat if a later layer conflicts
```

`gl-stack rebase --abort` restores every branch in the stack, not only the checked-out branch.
Because `init`/`rebase` enable rerere, a previously resolved conflict can be replayed through later
layers automatically.

## After a squash merge

A squash merge replaces a branch's commits with one trunk commit. `gl-stack sync` detects merged
parents and rebases descendants with `--onto`, avoiding duplicate replay:

```bash
gl-stack sync
gl-stack view --json
gl-stack sync --prune           # optionally remove merged local branches
```

If `sync` reports a conflict, it has already restored the branches. Run `gl-stack rebase`, resolve,
stage, and continue.

## Local and GitLab chains diverged

Divergence means local `.git/gl-stack` ordering and GitLab MR targets changed independently. A
non-interactive `sync` aborts before pushing or changing MRs.

Keep GitLab's remote chain:

```bash
gl-stack unstack --local
gl-stack checkout <stack-number-or-mr-iid>
```

Keep local ordering:

```bash
gl-stack unstack
gl-stack submit --auto
```

Neither path deletes MRs or branches. `unstack` without `--local` dissolves remote grouping by
retargeting the chain; rerunning `submit` recreates MR targets from local stack metadata.

## Restructuring a stack

`modify` is TUI-only, so an agent must rebuild explicitly:

```bash
gl-stack unstack
# Rename/drop branches and rewrite their Git ancestry as required.
gl-stack init --base main branch-1 branch-2 branch-3
gl-stack submit --auto
```

`init` adopts existing branches. Existing MRs survive; once ancestry is correct, `submit` updates
their targets and recreates the chain. Metadata alone does not reorder commits—rewrite branch
ancestry before `init`.

Before a manual reorder, preserve each old boundary SHA and inspect its unique range with
`git log <old-parent>..<branch>`. Replay ranges bottom-to-top with `git rebase --onto`; verify the
result before dissolving the original tracking.

## Branch belongs to multiple stacks (exit 6)

A trunk can belong to several local stacks, so commands that infer a stack can be ambiguous. Check
out a branch unique to the intended stack, or pass an explicit stack/MR target to commands that
support one:

```bash
gl-stack checkout <unique-branch>
gl-stack merge <stack-number> --yes
```

## Working from another tool or worktree

`gl-stack link` manages the GitLab MR chain without local metadata:

```bash
gl-stack link branch-a branch-b branch-c
gl-stack link --base develop --open a b c
gl-stack link 10 20 30
gl-stack link 7 feature-d
```

Arguments are bottom-to-top. Since no `.git/gl-stack` state is written, navigation does not work on
that result. Import it later with `gl-stack checkout <stack-number-or-mr>`.

## Stack state lock (exit 8)

Another process holds `.git/gl-stack.lock`, or the state file changed since this process loaded it.
Wait for the other command to finish and rerun. Do not delete the lock while a `gl-stack` process is
active.

## Interrupted modify session (exit 10)

If a human left a modify operation in recovery state:

```bash
# after resolving and staging a modify conflict
gl-stack modify --continue

# or restore the original stack
gl-stack modify --abort
```

Do not run bare `gl-stack modify` from an agent; it requires an interactive terminal.
