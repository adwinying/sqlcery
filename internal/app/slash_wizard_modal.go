package app

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/tui"
)

// slashWizardModal implements Modal for the guided slash command wizard.
// It owns the full SlashCommandWizardContext; no wizard state lives on
// InteractionState.
type slashWizardModal struct {
	wizard        SlashCommandWizardContext
	hScrollOffset int
	vpCommand     int // lazy viewport start for command step
	vpTarget      int // lazy viewport start for target step
	vpColumn      int // lazy viewport start for column step
}

func (s *slashWizardModal) Name() AppModal { return ModalSlashWizard }

func (s *slashWizardModal) FilterText() string {
	if s.wizard.Step != SlashCommandWizardStepTarget {
		return ""
	}
	return s.wizard.TargetFilter + "█"
}

func (s *slashWizardModal) FilterLabel() string { return "Filter:" }

func (s *slashWizardModal) Title() string {
	switch s.wizard.Step {
	case SlashCommandWizardStepTarget:
		return "Choose Table"
	case SlashCommandWizardStepColumn:
		return "Choose Columns"
	default:
		return "Choose Command"
	}
}

func (s *slashWizardModal) CounterText(_ InteractionState) string {
	switch s.wizard.Step {
	case SlashCommandWizardStepColumn:
		if len(s.wizard.Columns) == 0 {
			return ""
		}
		selected := clampWizardIndex(s.wizard.SelectedColumnCursor, len(s.wizard.Columns))
		return fmt.Sprintf("%d of %d", selected+1, len(s.wizard.Columns))
	case SlashCommandWizardStepTarget:
		filtered := filterWizardTargets(s.wizard.Targets, s.wizard.TargetFilter)
		if len(filtered) == 0 {
			return ""
		}
		selected := clampWizardIndex(s.wizard.SelectedTarget, len(filtered))
		return fmt.Sprintf("%d of %d", selected+1, len(filtered))
	default:
		if len(s.wizard.Commands) == 0 {
			return ""
		}
		selected := clampWizardIndex(s.wizard.SelectedCommand, len(s.wizard.Commands))
		return fmt.Sprintf("%d of %d", selected+1, len(s.wizard.Commands))
	}
}

func (s *slashWizardModal) StatusBarHints(_ InteractionState) []string {
	keys := defaultCommandModeKeys()
	switch s.wizard.Step {
	case SlashCommandWizardStepColumn:
		return []string{"enter confirm", "esc back", "ctrl+n next", "ctrl+p prev", "space toggle", "a toggle all", bindingSummary(keys.Help)}
	default:
		escHint := "esc close"
		if s.wizard.Step == SlashCommandWizardStepTarget && !s.wizard.DirectInvocation {
			escHint = "esc back"
		}
		return []string{"enter confirm", escHint, "ctrl+n next", "ctrl+p prev", "alt+← → scroll", bindingSummary(keys.Help)}
	}
}

func (s *slashWizardModal) HandleKey(msg tea.KeyPressMsg, ctx ModalContext) ModalResult {
	keys := defaultCommandModeKeys()

	switch {
	case msg.String() == "ctrl+c":
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}
	case key.Matches(msg, keys.Help):
		return modalResultForward{cmd: func() tea.Msg { return toggleHelpIntentMsg{} }}
	case key.Matches(msg, keys.Cancel):
		return s.handleEsc(ctx)
	case key.Matches(msg, keys.Submit), msg.String() == "enter":
		return s.submit(ctx)
	case key.Matches(msg, keys.NextSuggestion), msg.String() == "ctrl+n":
		return s.move(ctx, 1)
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "ctrl+p":
		return s.move(ctx, -1)
	case s.wizard.Step == SlashCommandWizardStepColumn && msg.String() == "space":
		return s.toggleColumn()
	case s.wizard.Step == SlashCommandWizardStepColumn && msg.String() == "a":
		return s.toggleAllColumns()
	case msg.String() == "alt+right":
		s.hScrollOffset += 8
		return modalResultNone{}
	case msg.String() == "alt+left":
		s.hScrollOffset = max(0, s.hScrollOffset-8)
		return modalResultNone{}
	case s.wizard.Step == SlashCommandWizardStepTarget &&
		(msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete"):
		return s.updateFilter(ctx, trimLastRune(s.wizard.TargetFilter))
	case s.wizard.Step == SlashCommandWizardStepTarget && msg.String() == "space":
		return s.updateFilter(ctx, s.wizard.TargetFilter+" ")
	case s.wizard.Step == SlashCommandWizardStepTarget &&
		len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt):
		return s.updateFilter(ctx, s.wizard.TargetFilter+msg.Text)
	default:
		return modalResultNone{}
	}
}

func (s *slashWizardModal) Render(_ InteractionState, innerWidth int) string {
	return renderSlashWizardContext(&s.wizard, s.wizardViewportStart(), &s.hScrollOffset, innerWidth)
}

func (s *slashWizardModal) handleEsc(ctx ModalContext) ModalResult {
	switch s.wizard.Step {
	case SlashCommandWizardStepColumn:
		s.wizard.Step = SlashCommandWizardStepTarget
		s.wizard.Columns = nil
		s.wizard.SelectedColumnCursor = 0
		return modalResultReady{status: "", level: NotificationNone}
	case SlashCommandWizardStepTarget:
		if strings.TrimSpace(s.wizard.TargetFilter) != "" {
			return s.updateFilter(ctx, "")
		}
		if s.wizard.DirectInvocation {
			return modalResultReady{status: "", level: NotificationNone, dismiss: true}
		}
		s.wizard.Step = SlashCommandWizardStepCommand
		s.wizard.Targets = nil
		s.wizard.SelectedTarget = 0
		return modalResultReady{status: "", level: NotificationNone}
	default:
		return modalResultReady{status: "", level: NotificationNone, dismiss: true}
	}
}

func (s *slashWizardModal) submit(ctx ModalContext) ModalResult {
	selectedCommand, ok := slashWizardCommandByIndex(&s.wizard)
	if !ok {
		return modalResultReady{status: "Slash command wizard is empty.", level: NotificationInfo, dismiss: true}
	}

	if s.wizard.Step == SlashCommandWizardStepColumn {
		return s.submitColumnStep(ctx, selectedCommand)
	}

	if selectedCommand.NeedsTarget {
		if s.wizard.Step != SlashCommandWizardStepTarget {
			nextWizard, err := buildSlashWizardFromCommand(context.Background(), slashCommandContext{
				Session: ctx.Session,
				Dialect: ctx.Dialect,
				Query:   ctx.Interaction,
			}, s.wizard.Commands, selectedCommand, s.wizard.SelectedCommand)
			if err != nil {
				return modalResultReady{
					status:      fmt.Sprintf("/commands failed: %v", err),
					level:       NotificationError,
					dismiss:     true,
					clearResult: true,
				}
			}
			if nextWizard == nil || len(nextWizard.Targets) == 0 {
				return modalResultReady{
					status:  fmt.Sprintf("/commands: no tables available for %s.", selectedCommand.DisplayName),
					level:   NotificationError,
					dismiss: true,
				}
			}
			s.wizard = *nextWizard
			s.vpTarget = 0
			return modalResultReady{status: "", level: NotificationNone}
		}

		selectedTarget, ok := slashWizardFilteredTargetByIndex(&s.wizard)
		if !ok {
			return modalResultReady{status: fmt.Sprintf("/commands: choose a table for %s.", selectedCommand.DisplayName), level: NotificationInfo}
		}

		if selectedCommand.NeedsColumns {
			nextWizard, err := buildSlashWizardColumnStep(context.Background(), slashCommandContext{
				Session: ctx.Session,
				Dialect: ctx.Dialect,
				Query:   ctx.Interaction,
			}, s.wizard, selectedTarget)
			if err != nil {
				return modalResultReady{
					status:  fmt.Sprintf("/commands failed loading columns: %v", err),
					level:   NotificationError,
					dismiss: true,
				}
			}
			if nextWizard == nil || len(nextWizard.Columns) == 0 {
				parsed := buildSlashWizardCommand(selectedCommand, &selectedTarget)
				return s.executeCommand(ctx, parsed)
			}
			s.wizard = *nextWizard
			s.vpColumn = 0
			return modalResultReady{status: "", level: NotificationNone}
		}

		parsed := buildSlashWizardCommand(selectedCommand, &selectedTarget)
		return s.executeCommand(ctx, parsed)
	}

	parsed := buildSlashWizardCommand(selectedCommand, nil)
	return s.executeCommand(ctx, parsed)
}

func (s *slashWizardModal) submitColumnStep(ctx ModalContext, selectedCommand SlashCommandWizardCommand) ModalResult {
	selectedCount := 0
	for _, col := range s.wizard.Columns {
		if col.Selected {
			selectedCount++
		}
	}
	if selectedCount == 0 {
		return modalResultReady{status: "Select at least one column.", level: NotificationInfo}
	}

	selectedTarget, ok := slashWizardFilteredTargetByIndex(&s.wizard)
	if !ok {
		return modalResultReady{status: "No table selected.", level: NotificationInfo, dismiss: true}
	}

	table := parseSlashTableRef(selectedTarget.Value)
	sql := buildSelectSQL(ctx.Dialect, table, s.wizard.Columns)
	return modalResultExecute{
		label:  selectedCommand.DisplayName,
		status: fmt.Sprintf("Dispatching %s from wizard.", selectedCommand.DisplayName),
		level:  NotificationInfo,
		execute: replaceEditorCmd(slashCommandResult{
			Status:        slashTemplateStatus(selectedCommand.DisplayName, selectedTarget.Display),
			ReplaceEditor: sql,
			ShouldReplace: true,
		}),
	}
}

func (s *slashWizardModal) executeCommand(ctx ModalContext, parsed slashCommand) ModalResult {
	return modalResultExecute{
		label:  parsed.DisplayName,
		status: fmt.Sprintf("Dispatching %s from wizard.", parsed.DisplayName),
		level:  NotificationInfo,
		execute: executeSlashCommandCmd(slashCommandContext{
			Session: ctx.Session,
			Dialect: ctx.Dialect,
			Query:   ctx.Interaction,
		}, parsed),
	}
}

func (s *slashWizardModal) move(_ ModalContext, delta int) ModalResult {
	s.hScrollOffset = 0
	switch s.wizard.Step {
	case SlashCommandWizardStepColumn:
		if len(s.wizard.Columns) == 0 {
			return modalResultNone{}
		}
		s.wizard.SelectedColumnCursor = wrapSelection(s.wizard.SelectedColumnCursor+delta, len(s.wizard.Columns))
		s.vpColumn = lazyScroll(s.wizard.SelectedColumnCursor, s.vpColumn, tui.ModalFixedRows-3)
		return modalResultNone{}
	case SlashCommandWizardStepTarget:
		filtered := filterWizardTargets(s.wizard.Targets, s.wizard.TargetFilter)
		if len(filtered) == 0 {
			return modalResultNone{}
		}
		s.wizard.SelectedTarget = wrapSelection(s.wizard.SelectedTarget+delta, len(filtered))
		listRows := tui.ModalSplitListRows - 2
		if s.wizard.DirectInvocation {
			listRows = tui.ModalSplitListRows - 1
		}
		s.vpTarget = lazyScroll(s.wizard.SelectedTarget, s.vpTarget, listRows)
		return modalResultNone{}
	default:
		if len(s.wizard.Commands) == 0 {
			return modalResultNone{}
		}
		s.wizard.SelectedCommand = wrapSelection(s.wizard.SelectedCommand+delta, len(s.wizard.Commands))
		s.vpCommand = lazyScroll(s.wizard.SelectedCommand, s.vpCommand, tui.ModalFixedRows-1)
		return modalResultNone{}
	}
}

func (s *slashWizardModal) toggleColumn() ModalResult {
	if len(s.wizard.Columns) == 0 {
		return modalResultNone{}
	}
	i := clampWizardIndex(s.wizard.SelectedColumnCursor, len(s.wizard.Columns))
	s.wizard.Columns[i].Selected = !s.wizard.Columns[i].Selected
	return modalResultNone{}
}

func (s *slashWizardModal) toggleAllColumns() ModalResult {
	allSelected := true
	for _, col := range s.wizard.Columns {
		if !col.Selected {
			allSelected = false
			break
		}
	}
	target := !allSelected
	for i := range s.wizard.Columns {
		s.wizard.Columns[i].Selected = target
	}
	return modalResultNone{}
}

func (s *slashWizardModal) updateFilter(_ ModalContext, filter string) ModalResult {
	s.wizard.TargetFilter = filter
	s.wizard.SelectedTarget = 0
	s.vpTarget = 0
	s.hScrollOffset = 0
	filtered := filterWizardTargets(s.wizard.Targets, filter)
	if len(filtered) == 0 {
		return modalResultReady{status: fmt.Sprintf("No tables match %q.", filter), level: NotificationInfo}
	}
	return modalResultNone{}
}

// wizardCurrentListLen returns the number of selectable items in the current step.
// Returns 0 if the current step has no selectable list (e.g. column step has columns,
// target step has filtered targets, command step has commands).
func (s *slashWizardModal) wizardCurrentListLen() int {
	switch s.wizard.Step {
	case SlashCommandWizardStepColumn:
		return len(s.wizard.Columns)
	case SlashCommandWizardStepTarget:
		return len(filterWizardTargets(s.wizard.Targets, s.wizard.TargetFilter))
	default:
		return len(s.wizard.Commands)
	}
}

// wizardViewportStart returns the stored lazy-scroll viewport top for the
// current step. Used by both Render (via renderSlashWizardContext) and
// HandleMouse for click-offset → item-index mapping.
func (s *slashWizardModal) wizardViewportStart() int {
	switch s.wizard.Step {
	case SlashCommandWizardStepColumn:
		return s.vpColumn
	case SlashCommandWizardStepTarget:
		return s.vpTarget
	default:
		return s.vpCommand
	}
}

// HandleMouse implements Modal.HandleMouse for slashWizardModal.
func (s *slashWizardModal) HandleMouse(msg tea.MouseClickMsg, ctx ModalContext) ModalResult {
	if ctx.MouseListOffset < 0 {
		return modalResultNone{}
	}
	n := s.wizardCurrentListLen()
	if n == 0 {
		return modalResultNone{}
	}
	vpStart := s.wizardViewportStart()
	idx := vpStart + ctx.MouseListOffset
	if idx < 0 || idx >= n {
		return modalResultNone{}
	}

	// Set the selection for the current step.
	switch s.wizard.Step {
	case SlashCommandWizardStepColumn:
		s.wizard.SelectedColumnCursor = idx
	case SlashCommandWizardStepTarget:
		s.wizard.SelectedTarget = idx
	default:
		s.wizard.SelectedCommand = idx
	}

	if ctx.MouseDoubleClick {
		return s.submit(ctx)
	}
	return modalResultNone{}
}

// HandleMouseWheel implements Modal.HandleMouseWheel for slashWizardModal.
// Clamps at the list boundaries — does not wrap.
func (s *slashWizardModal) HandleMouseWheel(_ ModalContext, msg tea.MouseWheelMsg) ModalResult {
	var delta int
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return modalResultNone{}
	}
	s.hScrollOffset = 0
	switch s.wizard.Step {
	case SlashCommandWizardStepColumn:
		if len(s.wizard.Columns) == 0 {
			return modalResultNone{}
		}
		s.wizard.SelectedColumnCursor = min(max(s.wizard.SelectedColumnCursor+delta, 0), len(s.wizard.Columns)-1)
		s.vpColumn = lazyScroll(s.wizard.SelectedColumnCursor, s.vpColumn, tui.ModalFixedRows-3)
	case SlashCommandWizardStepTarget:
		filtered := filterWizardTargets(s.wizard.Targets, s.wizard.TargetFilter)
		if len(filtered) == 0 {
			return modalResultNone{}
		}
		s.wizard.SelectedTarget = min(max(s.wizard.SelectedTarget+delta, 0), len(filtered)-1)
		listRows := tui.ModalSplitListRows - 2
		if s.wizard.DirectInvocation {
			listRows = tui.ModalSplitListRows - 1
		}
		s.vpTarget = lazyScroll(s.wizard.SelectedTarget, s.vpTarget, listRows)
	default:
		if len(s.wizard.Commands) == 0 {
			return modalResultNone{}
		}
		s.wizard.SelectedCommand = min(max(s.wizard.SelectedCommand+delta, 0), len(s.wizard.Commands)-1)
		s.vpCommand = lazyScroll(s.wizard.SelectedCommand, s.vpCommand, tui.ModalFixedRows-1)
	}
	return modalResultNone{}
}
