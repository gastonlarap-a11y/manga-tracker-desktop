@AGENTS.md

## Config maintenance
- After ANY task that changed structure, commands or conventions: check that this file — and
  AGENTS.md — still matches reality; propose the exact edit in the same session.
- Same-session fix also when a documented command fails, a stated convention contradicts the
  code, or the user corrects the same thing twice.
- New repeated procedure → propose a `.claude/skills/` entry; new language/area convention →
  a `paths:`-scoped rule in `.claude/rules/` — never more always-loaded lines.
- AGENTS.md says this repo is a scaffold. That line is a claim about reality: remove it in
  the same commit that makes it false, not later.
