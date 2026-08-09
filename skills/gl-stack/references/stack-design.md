# Designing a stack

Read this before running `gl-stack init`.

## Plan layers before writing code

A stack is a dependency chain. If one layer depends on another, the dependency must live in the same
branch or a lower branch. Plan first: there is no non-interactive in-place reorder, so fixing the
order later requires rewriting ancestry and rebuilding local tracking.

```text
(main) <- todo-app/models <- todo-app/api <- todo-app/frontend <- todo-app/integration
```

- `todo-app/models` — shared types and schema
- `todo-app/api` — routes that use the models
- `todo-app/frontend` — components that call the routes
- `todo-app/integration` — tests exercising the whole feature

Infer names from the real task; do not reuse these examples literally. If work warrants a stack,
create the stack before implementing every concern on one branch.

## Branch naming

Prefer a shared topic prefix and one concern: `<topic>/<concern>`, such as `billing/schema`,
`billing/api`, and `billing/ui`. Repository conventions override this suggestion.

Names are exact. `gl-stack add refactor/foo` creates `refactor/foo`. If `-m` is passed without a
branch name, the CLI derives a date-and-slug name from the commit message; explicit names are clearer.

## Stage deliberately

Use Git directly instead of the `add -Am` shortcut. Stage only the files that belong to the current
review layer, commit them, then create the next branch:

```bash
git add internal/models/user.go internal/models/session.go
git commit -m "Add user and session models"

gl-stack add api-routes
git add internal/api/routes.go internal/api/handlers.go
git commit -m "Add user API routes"
```

Multiple commits per branch are fine when they serve the same concern. `gl-stack add <branch>` does
not touch the working tree, so uncommitted changes carry onto the new branch; commit or stash first
when the next layer must start clean.

## Add a layer when the concern changes

Add a branch when starting a different concern that depends on completed lower work. Signals:

- moving from core logic to an adapter, API, UI, tests, or documentation;
- the next change has a different reviewer audience;
- the current layer is already large enough to review independently.

A layer that cannot be described in one sentence is usually two layers.

## One stack, one story

A reviewer should be able to walk bottom-to-top and see one feature built coherently. Use one stack
when every branch serves that story. Start a separate stack for unrelated features, bug fixes, or
refactors; do not combine work merely because it happened in the same session.

Use `gl-stack init <branch>` for a new effort or `gl-stack checkout <target>` to move between stacks.
A trivial incidental fix can share a layer; once it becomes a separate project, give it a separate
stack.
