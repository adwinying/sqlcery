# Triage Labels

| Role                | Label             |
|---------------------|-------------------|
| Needs evaluation    | `needs-triage`    |
| Waiting on reporter | `needs-info`      |
| Ready for AFK agent | `ready-for-agent` |
| Needs human impl    | `ready-for-human` |
| Will not action     | `wontfix`         |

## State machine

    new issue       → needs-triage
    needs-triage    → needs-info        (more info required)
    needs-triage    → ready-for-agent   (fully specified, agent-safe)
    needs-triage    → ready-for-human   (needs human judgment)
    needs-triage    → wontfix           (out of scope)
    needs-info      → needs-triage      (reporter replies)

Labels are mutually exclusive — remove the current one before applying the next.
