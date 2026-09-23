# Issue tracker: GitHub

Issues and specs live in https://github.com/Zendevve/Astradew/issues. Use the `gh` CLI for tracker operations.

Always pass `--repo Zendevve/Astradew` to `gh issue` and `gh pr` commands. For `gh api`, use explicit `repos/Zendevve/Astradew/...` endpoints. This works without a local Git remote.

## Conventions

- **Create**: `gh issue create --repo Zendevve/Astradew --title "<title>" --body-file <path>`. Use a UTF-8 Markdown file for multi-line bodies.
- **Read**: `gh issue view <number> --repo Zendevve/Astradew --json number,title,body,labels,comments`.
- **List**: `gh issue list --repo Zendevve/Astradew --state open --json number,title,body,labels,comments`, with appropriate `--label`, `--state`, and `--limit` options.
- **Comment**: `gh issue comment <number> --repo Zendevve/Astradew --body-file <path>`.
- **Apply / remove labels**: `gh issue edit <number> --repo Zendevve/Astradew --add-label "<label>"` / `--remove-label "<label>"`. Use the mapping in `triage-labels.md`.
- **Close**: `gh issue close <number> --repo Zendevve/Astradew --comment "<reason>"`.

## Pull requests as a triage surface

**PRs as a request surface: no.** _(Set to `yes` if this repo treats external PRs as feature requests; `/triage` reads this flag.)_

When enabled, use the same labels and states as issues:

- **Read**: `gh pr view <number> --repo Zendevve/Astradew --comments` and `gh pr diff <number> --repo Zendevve/Astradew`.
- **List external PRs**: `gh api --paginate "repos/Zendevve/Astradew/pulls?state=open&per_page=100"`. Keep PRs whose `author_association` is `CONTRIBUTOR`, `FIRST_TIME_CONTRIBUTOR`, or `NONE`.
- **Comment / label / close**: use `gh pr comment`, `gh pr edit --add-label` / `--remove-label`, and `gh pr close`, always with `--repo Zendevve/Astradew`.

GitHub shares one number space across issues and PRs. Resolve an ambiguous reference with `gh pr view <number> --repo Zendevve/Astradew`; if it is not a PR, use `gh issue view`.

## When a skill says "publish to the issue tracker"

Create a GitHub issue using the create command above.

## When a skill says "fetch the relevant ticket"

Use the read command above to fetch the issue body, labels, and comments.

## Wayfinding operations

Used by `/wayfinder`. The map is a single issue with child issues as tickets.

- **Map**: an issue labelled `wayfinder:map`, holding Notes / Decisions-so-far / Fog.
- **Child ticket**: link the issue to the map using GitHub's sub-issues API. Where sub-issues are unavailable, add the child to a task list in the map and put `Part of #<map>` at the top of the child body. Use `wayfinder:<type>` labels: `research`, `prototype`, `grilling`, or `task`.
- **Blocking**: use GitHub's native issue dependencies. Add an edge with `gh api --method POST repos/Zendevve/Astradew/issues/<child>/dependencies/blocked_by -F issue_id=<blocker-db-id>`. Obtain the database ID with `gh api repos/Zendevve/Astradew/issues/<blocker-number> --jq .id`; it is not the issue number or node ID. Where dependencies are unavailable, use a `Blocked by: #<n>, #<n>` line in the child body.
- **Frontier**: list the map's open children. Exclude children with an assignee or open blockers (`issue_dependencies_summary.blocked_by > 0`, or an open issue in the `Blocked by` line). First in map order wins.
- **Claim**: `gh issue edit <number> --repo Zendevve/Astradew --add-assignee @me`, before doing any work.
- **Resolve**: comment with the answer, close the child issue, then append a context pointer (gist and link) to the map's Decisions-so-far.
