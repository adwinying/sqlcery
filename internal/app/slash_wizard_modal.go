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

func (s *slashWizardModal) HandleKey(msg tea.KeyPressMsg, m *Model) tea.Cmd {
	keys := m.command.KeyMap()

	switch {
	case msg.String() == "ctrl+c":
		m.closeModal()
		m.state.SetReady("Closed the slash command wizard.")
		return nil
	case key.Matches(msg, keys.Cancel):
		return s.handleEsc(m)
	case key.Matches(msg, keys.Submit), msg.String() == "enter":
		return s.submit(m)
	case key.Matches(msg, keys.NextSuggestion), msg.String() == "ctrl+n":
		s.move(m, 1)
		return nil
	case key.Matches(msg, keys.PrevSuggestion), msg.String() == "ctrl+p":
		s.move(m, -1)
		return nil
	case s.wizard.Step == SlashCommandWizardStepTarget &&
		(msg.String() == "backspace" || msg.String() == "ctrl+h" || msg.String() == "delete"):
		s.updateFilter(m, trimLastRune(s.wizard.TargetFilter))
		return nil
	case s.wizard.Step == SlashCommandWizardStepTarget && msg.String() == "space":
		s.updateFilter(m, s.wizard.TargetFilter+" ")
		return nil
	case s.wizard.Step == SlashCommandWizardStepTarget &&
		len(msg.Text) > 0 && !msg.Mod.Contains(tea.ModAlt):
		s.updateFilter(m, s.wizard.TargetFilter+msg.Text)
		return nil
	default:
		return nil
	}
}

func (s *slashWizardModal) Render(_ InteractionState) string {
	return renderSlashWizardContext(&s.wizard)
}

func (s *slashWizardModal) handleEsc(m *Model) tea.Cmd {
	if s.wizard.Step == SlashCommandWizardStepTarget {
		if strings.TrimSpace(s.wizard.TargetFilter) != "" {
			s.updateFilter(m, "")
			return nil
		}
		if s.wizard.DirectInvocation {
			m.closeModal()
			m.state.SetReady("Closed the slash command wizard.")
			return nil
		}
		s.wizard.Step = SlashCommandWizardStepCommand
		s.wizard.Targets = nil
		s.wizard.SelectedTarget = 0
		selectedCommand, _ := slashWizardCommandByIndex(&s.wizard)
		m.state.SetReady(fmt.Sprintf("Choose a command. %s is selected.", selectedCommand.DisplayName))
		return nil
	}
	m.closeModal()
	m.state.SetReady("Closed the slash command wizard.")
	return nil
}

func (s *slashWizardModal) submit(m *Model) tea.Cmd {
	selectedCommand, ok := slashWizardCommandByIndex(&s.wizard)
	if !ok {
		m.closeModal()
		m.state.SetReady("Slash command wizard is empty.")
		return nil
	}

	if selectedCommand.NeedsTarget {
		if s.wizard.Step != SlashCommandWizardStepTarget {
			nextWizard, err := buildSlashWizardFromCommand(context.Background(), slashCommandContext{
				Session: m.session,
				Dialect: m.adapterDialect(),
				Query:   m.state.Interaction.snapshot(),
			}, s.wizard.Commands, selectedCommand, s.wizard.SelectedCommand)
			if err != nil {
				m.closeModal()
				m.state.SetReady(fmt.Sprintf("/commands failed: %v", err))
				m.state.SetLatestResultContext(nil)
				return nil
			}
			if nextWizard == nil || len(nextWizard.Targets) == 0 {
				m.closeModal()
				m.state.SetReady(fmt.Sprintf("/commands: no tables available for %s.", selectedCommand.DisplayName))
				return nil
			}
			s.wizard = *nextWizard
			m.state.SetReady(fmt.Sprintf("Choose a table for %s and press enter.", selectedCommand.DisplayName))
			return nil
		}

		selectedTarget, ok := slashWizardFilteredTargetByIndex(&s.wizard)
		if !ok {
			m.state.SetReady(fmt.Sprintf("/commands: choose a table for %s.", selectedCommand.DisplayName))
			return nil
		}

		parsed := buildSlashWizardCommand(selectedCommand, &selectedTarget)
		m.closeModal()
		return m.startExecution(parsed.DisplayName,
			fmt.Sprintf("Dispatching %s from wizard.", parsed.DisplayName),
			executeSlashCommandCmd(slashCommandContext{
				Session: m.session,
				Dialect: m.adapterDialect(),
				Query:   m.state.Interaction.snapshot(),
			}, parsed))
	}

	parsed := buildSlashWizardCommand(selectedCommand, nil)
	m.closeModal()
	return m.startExecution(parsed.DisplayName,
		fmt.Sprintf("Dispatching %s from wizard.", parsed.DisplayName),
		executeSlashCommandCmd(slashCommandContext{
			Session: m.session,
			Dialect: m.adapterDialect(),
			Query:   m.state.Interaction.snapshot(),
		}, parsed))
}

func (s *slashWizardModal) move(m *Model, delta int) {
	switch s.wizard.Step {
	case SlashCommandWizardStepTarget:
		filtered := filterWizardTargets(s.wizard.Targets, s.wizard.TargetFilter)
		if len(filtered) == 0 {
			return
		}
		s.wizard.SelectedTarget = wrapSelection(s.wizard.SelectedTarget+delta, len(filtered))
		m.state.SetReady(fmt.Sprintf("Selected table %s.", filtered[s.wizard.SelectedTarget].Display))
	default:
		if len(s.wizard.Commands) == 0 {
			return
		}
		s.wizard.SelectedCommand = wrapSelection(s.wizard.SelectedCommand+delta, len(s.wizard.Commands))
		selectedCommand, _ := slashWizardCommandByIndex(&s.wizard)
		m.state.SetReady(fmt.Sprintf("Selected %s.", selectedCommand.DisplayName))
	}
}

func (s *slashWizardModal) updateFilter(m *Model, filter string) {
	s.wizard.TargetFilter = filter
	s.wizard.SelectedTarget = 0
	filtered := filterWizardTargets(s.wizard.Targets, filter)
	if len(filtered) == 0 {
		m.state.SetReady(fmt.Sprintf("No tables match %q.", filter))
	} else {
		m.state.SetReady(fmt.Sprintf("%d table(s) match filter.", len(filtered)))
	}
}
