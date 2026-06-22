# ANSI 16-color terminal-aware theme

The app's color theme uses only ANSI 0–15 color slots (plain `lipgloss.Color`) instead of fixed 256-color palette entries wrapped in `lipgloss.AdaptiveColor`. The terminal emulator's own theme resolves each slot to its actual color, so the app automatically adapts to any user theme (Catppuccin, Solarized, Gruvbox, Nord, Dracula, etc.) without any configuration.

The mapping chosen:

| Role                 | ANSI slot               |
| ---                  | ---                     |
| `accent`             | 12 (bright blue)        |
| `accentSoft`         | 6 (cyan)                |
| `accentWarm`         | 3 (yellow)              |
| `success`            | 2 (green)               |
| `danger`             | 1 (red)                 |
| `muted`              | 8 (bright black / gray) |
| `selectedBackground` | 4 (blue)                |
| `selectedForeground` | 15 (bright white)       |

`panelForeground` and `statusBarForeground` are dropped entirely — styles that used them are left unstyled, inheriting the terminal's default foreground.

`mutedSoft` is collapsed into `muted`. The original shade difference (256-color 243 vs 246) was imperceptible and cannot be expressed faithfully with 16 colors; the remaining visual distinction between line numbers, comments, ghost text, and inactive borders comes from italic/bold modifiers already applied to those styles.

**Considered options rejected:**

- *Keep 256-color palette with `AdaptiveColor`:* The hardcoded values clashed with custom terminal themes — the app imposed its own colour taste rather than integrating with the user's environment.
- *User-configurable theme:* Adds a config surface and ongoing maintenance burden. ANSI 16-color achieves the same result automatically because the terminal already is the configuration layer.
- *Bright variants (ANSI 9–15) for all roles:* ANSI 9–14 are repurposed unpredictably across themes (e.g. ANSI 12 is purple in Dracula, gray in Solarized). Non-bright slots 1–8 are more reliably semantic.

**Consequences:**

- The app looks correct on any well-formed 16-color terminal theme with no configuration required.
- `lipgloss.AdaptiveColor` is no longer used in the theme; light/dark adaptation is delegated entirely to the terminal.
- On terminals with poorly configured or default ANSI palettes (e.g. unthemed xterm), some colors may appear harsh. This is the terminal's concern, not the app's.
- ANSI 12 (bright blue) is used for `accent` because it was the most consistently "blue" slot across tested themes (Catppuccin, Gruvbox, Nord, Solarized, Dracula).
