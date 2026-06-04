# Domain Docs

## Layout

Single-context repo. One shared domain context for the entire codebase.

| Path         | Purpose                                                         |
|--------------|-----------------------------------------------------------------|
| `CONTEXT.md` | Domain glossary and project overview. Read before any task.     |
| `docs/adr/`  | Architectural Decision Records. Read before structural changes. |

## Consumer rules

1. Read `CONTEXT.md` before working on anything that touches domain concepts.
2. Check `docs/adr/` before proposing architecture changes — a prior decision may already exist.
3. Record significant architectural decisions as new ADRs under `docs/adr/`.
4. Use glossary terms from `CONTEXT.md` consistently in code, comments, and issues.
