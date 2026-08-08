# gl-stack

<img width="960" height="894" alt="demo" src="https://github.com/user-attachments/assets/a418532f-81fa-4978-9add-f44388bd5910" />


A CLI for managing stacked branches and merge requests on GitLab.

## GitLab authentication

`gl-stack` reads a GitLab access token from `GITLAB_TOKEN`, falling back to
`GLAB_TOKEN`:

```sh
export GITLAB_TOKEN="glpat-..."
```

For full functionality, including creating, retargeting, and merging merge
requests, the token needs write access to the GitLab API. The recommended
configuration is a **personal access token** with:

- the [`api`](https://docs.gitlab.com/security/tokens/access_token_scopes/) scope
- at least the **Developer** role in the project

The `read_api` scope is sufficient only for read-only operations such as listing
and viewing stacks. It cannot create or update merge requests.

Commands such as `submit`, `push`, and `sync` also run ordinary Git fetch and
push operations. Their authentication comes from the repository's existing Git
configuration; setting `GITLAB_TOKEN` does not automatically configure Git
credentials.

| Token and Git remote | Required token scopes |
| --- | --- |
| Personal access token with an SSH remote | `api` |
| Personal access token with an HTTPS remote using the same token | `api` |
| Project or group access token with an SSH remote | `api` |
| Project or group access token with an HTTPS remote using the same token | `api`, `write_repository` |
| Read-only stack access | `read_api` |

For personal access tokens, `api` includes repository access over Git-over-HTTP.
For project and group access tokens, `write_repository` is separate and does
not provide API access, so both scopes are required when the token handles API
requests and HTTPS pushes.

A Developer can generally create merge requests and push unprotected feature
branches. Merging still depends on the target branch's **Allowed to merge**
rule, required approvals, pipeline status, and other project merge checks. If a
protected target branch permits only Maintainers to merge, the token's user or
bot must have the **Maintainer** role.

No `sudo`, `admin_mode`, `read_user`, registry, or runner scopes are required.
See GitLab's documentation for [personal access tokens](https://docs.gitlab.com/user/profile/personal_access_tokens/)
and [project access tokens](https://docs.gitlab.com/user/project/settings/project_access_tokens/).
