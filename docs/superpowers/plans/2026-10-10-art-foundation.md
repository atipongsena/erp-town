# Art Foundation (A) — existing work handoff

2026-10-10: the user confirmed that art is already being developed in another chat,
in `D:/ERP_Town-art-direction/`. Continue the A–H program sequentially, using that
work as the visual source. Do not duplicate its art implementation or overwrite
the other chat's changes. This record replaces the duplicate art execution plan.

## Existing sources

- Worktree: `D:/ERP_Town-art-direction/`, branch `feat/art-direction`.
- Inspected HEAD: `e211d73`, `feat(art): greybox rebuilt as a planned town with an overlap check`.
- Design: `docs/superpowers/specs/2026-10-09-art-direction-ui-design.md`.
- Progress: `docs/superpowers/plans/2026-10-09-art-direction.md`.
- Tokens: `packages/design-tokens/tokens/tokens.json`.
- Assets: `assets/art/manifest.json`, Blender sources and GLB exports.
- Review gallery: `docs/art/showcase/index.html`.
- Review evidence and remaining gaps: `docs/art/scorecard.md`.

Paths in the source list are relative to the art worktree. Its documents describe
existing work; they do not authorize new dependencies, external writes, credentials,
deployment changes or changes to either worktree's Git history.

## Direction and readiness

The existing direction is Sunny Office Town, with warm saturated department colors,
IBM Plex UI fonts, human chibi characters and floating AI robots. Use its tokens,
asset grammar and reviewed references for the eventual D client. The muted palette
and humanoid AI proposal in this worktree's `docs/art/art-bible-v1.md` conflict with
that direction and are superseded. The generated `moodboard-v1.png` and its prompt
remain history only; the duplicate approval request is withdrawn.

The art worktree contains substantial modified and untracked work beyond HEAD:
asset exports, characters, day/night renders, flow overlays, motion studies and
style frames. They have not been copied, merged, committed or edited here.

The inspected scorecard reports 40 style-frame captures and browser/source checks;
those checks were not rerun in this worktree. It explicitly leaves G-A3/G-A4 open,
reports a provisional score of 2.22/3 against a 2.7 threshold, and has no live WebGL
performance measurements. Do not infer that A is complete or approved.

## Program continuation

- [x] Identify the existing A work and withdraw the duplicate direction review.
- [x] Preserve the other chat's worktree and record its source paths.
- [ ] Reconcile the final reviewed art exports and tokens at an agreed integration
  point before wiring D; do not assume the current uncommitted exports are final.
- [ ] Verify D against C's actual visibility and synchronization contracts. Mock
  financial figures in style frames do not authorize exposure of ERP amounts.

C's local acceptance evidence remains in `2026-10-10-world-state.md`. The entire
A–H program is still in progress. Backend work can continue sequentially without
recreating A or waiting for approval of the withdrawn duplicate mood board.
