package app

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// slashWizardModal implements Modal for the guided slash command wizard.
// It owns the full SlashCommandWizardContext; no wizard state lives on
// InteractionState.
type slashWizardModal struct {
	wizard SlashCommandWizardContext
}

func (s *slashWizardModal) Name() AppModal { return ModalSlashWizard }

func (s *slashWizardModal) HandleKey(msg tea.KeyPressMsg, ctx ModalContext) ModalResult {
	keys := defaultCommandModeKeys()

	switch {
	case msg.String() == "ctrl+c":
		return modalResultReady{status: "Closed the slash command wizard.", dismiss: true}
	case key.Matches(msg, keys.Cancel):
		return s.handleEsc(ctx)
	case key.Matches(msg, keys.Submit), msg.String() == "enter":
		return s.submit(ctx)
	case key.Matches(msg, keys.NextSuggestion), msg.String() == "ctrl+n":
		return s.move(ctx, 1)
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "ctrl+p":
		return s.move(ctx, -1)
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

func (s *slashWizardModal) Render(_ InteractionState) string {
	return renderSlashWizardContext(&s.wizard)
}

func (s *slashWizardModal) handleEsc(ctx ModalContext) ModalResult {
	if s.wizard.Step == SlashCommandWizardStepTarget {
		if strings.TrimSpace(s.wizard.TargetFilter) != "" {
			return s.updateFilter(ctx, "")
		}
		if s.wizard.DirectInvocation {
			return modalResultReady{status: "Closed the slash command wizard.", dismiss: true}
		}
		s.wizard.Step = SlashCommandWizardStepCommand
		s.wizard.Targets = nil
		s.wizard.SelectedTarget = 0
		selectedCommand, _ := slashWizardCommandByIndex(&s.wizard)
		return modalResultReady{status: fmt.Sprintf("Choose a command. %s is selected.", selectedCommand.DisplayName)}
	}
	return modalResultReady{status: "Closed the slash command wizard.", dismiss: true}
}

func (s *slashWizardModal) submit(ctx ModalContext) ModalResult {
	selectedCommand, ok := slashWizardCommandByIndex(&s.wizard)
	if !ok {
		return modalResultReady{status: "Slash command wizard is empty.", dismiss: true}
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
					dismiss:     true,
					clearResult: true,
				}
			}
			if nextWizard == nil || len(nextWizard.Targets) == 0 {
				return modalResultReady{
					status:  fmt.Sprintf("/commands: no tables available for %s.", selectedCommand.DisplayName),
					dismiss: true,
				}
			}
			s.wizard = *nextWizard
			return modalResultReady{status: fmt.Sprintf("Choose a table for %s and press enter.", selectedCommand.DisplayName)}
		}

		selectedTarget, ok := slashWizardFilteredTargetByIndex(&s.wizard)
		if !ok {
			return modalResultReady{status: fmt.Sprintf("/commands: choose a table for %s.", selectedCommand.DisplayName)}
		}

		parsed := buildSlashWizardCommand(selectedCommand, &selectedTarget)
		return modalResultExecute{
			label:  parsed.DisplayName,
			status: fmt.Sprintf("Dispatching %s from wizard.", parsed.DisplayName),
			execute: executeSlashCommandCmd(slashCommandContext{
				Session: ctx.Session,
				Dialect: ctx.Dialect,
				Query:   ctx.Interaction,
			}, parsed),
		}
	}

	parsed := buildSlashWizardCommand(selectedCommand, nil)
	return modalResultExecute{
		label:  parsed.DisplayName,
		status: fmt.Sprintf("Dispatching %s from wizard.", parsed.DisplayName),
		execute: executeSlashCommandCmd(slashCommandContext{
			Session: ctx.Session,
			Dialect: ctx.Dialect,
			Query:   ctx.Interaction,
		}, parsed),
	}
}

func (s *slashWizardModal) move(_ ModalContext, delta int) ModalResult {
	switch s.wizard.Step {
	case SlashCommandWizardStepTarget:
		filtered := filterWizardTargets(s.wizard.Targets, s.wizard.TargetFilter)
		if len(filtered) == 0 {
			return modalResultNone{}
		}
		s.wizard.SelectedTarget = wrapSelection(s.wizard.SelectedTarget+delta, len(filtered))
		return modalResultReady{status: fmt.Sprintf("Selected table %s.", filtered[s.wizard.SelectedTarget].Display)}
	default:
		if len(s.wizard.Commands) == 0 {
			return modalResultNone{}
		}
		s.wizard.SelectedCommand = wrapSelection(s.wizard.SelectedCommand+delta, len(s.wizard.Commands))
		selectedCommand, _ := slashWizardCommandByIndex(&s.wizard)
		return modalResultReady{status: fmt.Sprintf("Selected %s.", selectedCommand.DisplayName)}
	}
}

func (s *slashWizardModal) updateFilter(_ ModalContext, filter string) ModalResult {
	s.wizard.TargetFilter = filter
	s.wizard.SelectedTarget = 0
	filtered := filterWizardTargets(s.wizard.Targets, filter)
	if len(filtered) == 0 {
		return modalResultReady{status: fmt.Sprintf("No tables match %q.", filter)}
	}
	return modalResultReady{status: fmt.Sprintf("%d table(s) match filter.", len(filtered))}
}
