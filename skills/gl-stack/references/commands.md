# Command behavior

`gl-stack <command> --help` is authoritative for flags and arguments. This reference covers the
preconditions, side effects, ordering, and failure modes that matter to an agent.

## init

Creates local stack metadata and checks out the **last** branch in the list:

```bash
gl-stack init --base main auth api frontend
```

Arguments are bottom-to-top. Existing branches are adopted; missing branches are created from the
branch immediately below. With no arguments, `init` is interactive and must not be run by an agent.
`init` enables Git rerere (prompting the first time in a TTY); configure it beforehand for automation.

## add

- Must run from the current stack's top branch, or its trunk if the stack is empty.
- Without `-Am`, uncommitted changes carry onto the new branch.
- `add -Am` can commit in place when the current branch has no unique commits instead of creating a
  new layer.
- `-A` and `-u` are mutually exclusive and both require `-m`.

Prefer explicit `git add` and `git commit`, then `gl-stack add <name>`, so layer ownership is clear.

## push

Pushes every active (non-merged, non-queued) branch with per-branch `--force-with-lease`.

**Not atomic.** A remote lease rejection can occur after other branches updated. Fix the rejected
branch and rerun; already-current branches are harmless. `push` never creates or retargets MRs.

## submit

Pushes active branches, creates MRs that are missing, retargets existing MRs, and creates or updates
the GitLab target/source chain.

- Use `--auto` non-interactively. New MRs are drafts unless `--open` is passed.
- `--open` also marks existing MRs ready for review.
- A single-commit branch uses the commit subject and body for generated MR content; a multi-commit
  branch uses a humanized branch name for its title.
- The operation is not atomic. Earlier pushes or MR changes can remain if a later step fails. Fix the
  failure and rerun the same command.
- `submit` can link existing open MRs after a remote stack was dissolved.

## link

Creates or updates a GitLab stack chain without writing local `.git/gl-stack` state. Use it for
branches managed by another tool or worktree.

- Arguments are bottom-to-top branch names or MR IIDs.
- A numeric first argument is interpreted as an existing stack number when one exists, allowing
  `gl-stack link 7 feature-c` to append without relisting current MRs.
- Branch arguments are pushed, missing MRs are created, and wrong MR targets are corrected.
- Membership is additive. Arguments that would remove existing members are rejected.
- Merged, closed, queued, or auto-merge-enabled MRs cannot be newly added to a stack.

Because `link` writes no local state, navigation commands do not work until the stack is imported
with `gl-stack checkout <stack-number-or-mr>`.

## sync

Runs these steps in order:

1. Fetch the selected remote.
2. Reconcile the local stack with the GitLab MR target/source chain.
3. Fast-forward the trunk when possible.
4. Cascade-rebase branches whose parent moved.
5. Push all active branches atomically with leases.
6. Refresh MR state.
7. Ensure each open MR targets the branch immediately below it when at least two MRs exist.
8. Prune local merged branches only when `--prune` is passed.

`sync` never opens MRs. A non-interactive local/remote divergence aborts before pushing or changing
MRs. A rebase conflict restores all branches and exits 3; run `gl-stack rebase` to reproduce it in a
continuable form.

## rebase

Fetches and cascade-rebases the stack.

- `--upstack` rebases from the current branch toward the top. Use it after editing a lower layer.
- `--downstack` rebases from the trunk through the current branch.
- `--no-trunk` skips fetching and trunk movement, aligning stack branches only.
- `--continue` resumes after staged conflict resolutions; `--abort` restores all branches.
- Merged parent MRs are detected and replayed with `--onto` so squash merges are not duplicated.
- Queued branches are frozen, but descendants remain based on the queued branch.
- Starting while another rebase is active exits 7.

## view

- `--json` is the machine interface and never prompts.
- Bare `view` opens a TUI under a TTY.
- `--short` is non-interactive but human-formatted.
- Viewing refreshes MR state best-effort and persists that refreshed local metadata.

## checkout

Accepts a stack number, MR IID, MR URL, or branch name.

- A bare number resolves as a stack number first, then an MR IID, then a branch name.
- Remote identifiers discover the MR chain, fetch its branches, and create local stack tracking.
- A branch name resolves only against local tracking.
- A composition conflict with an existing local stack cannot be forced. Run
  `gl-stack unstack --local` on the conflicting local stack, then retry.
- Bare `checkout` opens an interactive local/remote picker and must not be used by an agent.

## unstack

Removes stack grouping only; it does not delete branches or MRs.

- No argument targets the active stack.
- A stack number can target a stack not currently checked out.
- `--local` removes only `.git/gl-stack` tracking and leaves the GitLab MR chain unchanged.
- Without `--local`, remote grouping is dissolved by retargeting MRs to the trunk, then local
  tracking is removed where present.

## merge

- Pass an MR IID to merge that MR and every unmerged MR below it, or a stack number to merge the
  whole stack. A bare number is stack-first.
- Each MR is retargeted to the trunk immediately before merging, bottom-to-top.
- **Not atomic.** If a later MR fails, earlier MRs remain merged.
- Each candidate must be open and not a draft. GitLab still enforces protection, approvals,
  pipelines, and repository rules.
- Method flags are `--squash`, `--merge`, or `--merge-method merge|squash`.
- Use `--yes` non-interactively.

## navigation

`up`, `down`, `top`, `bottom`, and `trunk` are non-interactive. `up` and `down` accept a count.
Movement clamps at stack bounds, and navigation skips merged branches when starting from an active
branch. `switch` is interactive only.
