# ADR 0015 — auto cleanup, opt-in, for the desktop app

**Status**: accepted (this fork) · **Date**: 2026-11-19 · **Amends**: 0001

## Context

ADR 0001 keeps every run worktree until someone runs `cleanup`, because an open
workspace is the only signal that a run is still unread. Under `backend: hc`
two things change:

- The app shows that signal itself: closing a run's space ends its terminals.
  A worktree with no live hc terminal in it has been looked at and put away.
- `cleanup` asked the Herdr TUI (`herdr worktree list`) whether a worktree's
  workspace is open. The TUI knows nothing of the app's spaces, so on a machine
  with both installed it called every app space closed.

## Decision

- Under hc, `cleanup.Scan` uses `hcRepos`: `git worktree list` for the
  worktrees, `hc ls` for the open ones (a live terminal whose folder is inside
  the worktree). If hc cannot answer, the scan fails; nothing is removed.
- A run worktree is removable only when all three hold: no live terminal in it,
  nothing uncommitted (`git status --porcelain` empty), and no commit on its
  branch missing from every other branch and remote. Work to review stays.
- `auto_cleanup: true` at the top of automations.yaml lets the daemon apply
  that plan every 10 minutes, skipped while any run is in flight. Off by
  default, so ADR 0001 still describes the default.
- The checkout is removed with `git worktree remove`, without `--force`, so git
  still refuses a dirty one. The branch is then deleted with `git branch -D`,
  only after the check that its commits are held elsewhere has run again.
- A run worktree is recognised by its `auto/` branch prefix alone; nothing
  records which worktrees a run created. Files git ignores (`.env`, build
  output) are not "uncommitted" and go with the checkout.

## Consequences

- With `auto_cleanup` on, a run that produced nothing disappears from disk
  within 10 minutes of its space being closed. One that committed work stays
  until the work is pushed or merged, or until `cleanup` is run by hand.
- A Herdr app that is closed leaves its terminals running in herdr-core, so its
  runs still count as open.
