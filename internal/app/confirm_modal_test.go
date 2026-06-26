package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/adwinying/sqlcery/internal/config"
)

// ---- modalConfirm unit tests ----

// buildConfirmModel creates a model with a modalConfirm pushed on top of a
// startup picker (or another base modal) so we can test push/pop in isolation.
func buildConfirmModel(t *testing.T, prompt string, onYes tea.Msg) Model {
	t.Helper()
	model := newStartupPickerModel(t, modelDependencies{
		connectionsLoader: func() (config.Connections, error) {
			return config.Connections{}, nil
		},
	})
	model.pushModal(&modalConfirm{prompt: prompt, onYes: onYes})
	return model
}

// pressKeyAndDriveCmds presses keyStr, collects any resulting commands,
// executes them, and drives the resulting messages back through Update.
// It is the cmd-driving counterpart to pressKey.
func pressKeyAndDriveCmds(t *testing.T, model Model, keyStr string) Model {
	t.Helper()
	var msg tea.KeyPressMsg
	switch keyStr {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEsc}
	case "backspace":
		msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+n":
		msg = tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
	case "ctrl+p":
		msg = tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	case "ctrl+c":
		msg = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	default:
		msg = tea.KeyPressMsg{Text: keyStr}
	}
	next, cmd := model.Update(msg)
	model = next.(Model)
	if cmd != nil {
		msgs := collectCommandMessagesForTest(t, cmd)
		for _, m := range msgs {
			next2, _ := model.Update(m)
			model = next2.(Model)
		}
	}
	return model
}

// sentinelMsg is an opaque message used in tests as the onYes continuation.
type sentinelMsg struct{ id int }

// TestConfirmYesForwardsAndPops verifies that pressing y pops the confirm and
// forwards the onYes continuation into the model's message loop.
func TestConfirmYesForwardsAndPops(t *testing.T) {
	var received tea.Msg
	// Push the confirm modal with a sentinel onYes.
	model := buildConfirmModel(t, "Test prompt?", sentinelMsg{id: 42})

	// Register a handler for sentinelMsg so we can observe it.
	// We drive the model manually: get the cmd from pressing y, run it, and
	// check whether it produces the sentinelMsg.
	next, cmd := model.Update(tea.KeyPressMsg{Text: "y"})
	model = next.(Model)

	// The confirm modal should be popped.
	if model.currentModal() != nil && model.currentModal().Name() == ModalConfirm {
		t.Fatal("confirm modal should be popped after y")
	}

	// The cmd should produce the sentinelMsg.
	if cmd == nil {
		t.Fatal("cmd = nil after y, want sentinelMsg forwarded")
	}
	msgs := collectCommandMessagesForTest(t, cmd)
	for _, m := range msgs {
		if _, ok := m.(sentinelMsg); ok {
			received = m
		}
	}
	if received == nil {
		t.Fatalf("no sentinelMsg in cmd messages %v after y", msgs)
	}
	if got, want := received.(sentinelMsg).id, 42; got != want {
		t.Fatalf("sentinelMsg.id = %d, want %d", got, want)
	}
}

// TestConfirmEnterForwardsAndPops verifies that pressing Enter also confirms.
func TestConfirmEnterForwardsAndPops(t *testing.T) {
	model := buildConfirmModel(t, "Confirm?", sentinelMsg{id: 7})

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(Model)

	if model.currentModal() != nil && model.currentModal().Name() == ModalConfirm {
		t.Fatal("confirm modal should be popped after Enter")
	}
	if cmd == nil {
		t.Fatal("cmd = nil after Enter, want sentinelMsg forwarded")
	}
	msgs := collectCommandMessagesForTest(t, cmd)
	var found bool
	for _, m := range msgs {
		if s, ok := m.(sentinelMsg); ok && s.id == 7 {
			found = true
		}
	}
	if !found {
		t.Fatalf("no sentinelMsg{id:7} in cmd messages %v after Enter", msgs)
	}
}

// TestConfirmNDeclinesPopsOnly verifies that pressing n pops the confirm
// without forwarding anything.
func TestConfirmNDeclinesPopsOnly(t *testing.T) {
	model := buildConfirmModel(t, "Confirm?", sentinelMsg{id: 1})
	stackBefore := len(model.modals)

	next, cmd := model.Update(tea.KeyPressMsg{Text: "n"})
	model = next.(Model)

	if model.currentModal() != nil && model.currentModal().Name() == ModalConfirm {
		t.Fatal("confirm modal should be popped after n")
	}
	if len(model.modals) != stackBefore-1 {
		t.Fatalf("modal stack len = %d, want %d after n", len(model.modals), stackBefore-1)
	}
	// No continuation should be forwarded.
	if cmd != nil {
		msgs := collectCommandMessagesForTest(t, cmd)
		for _, m := range msgs {
			if _, ok := m.(sentinelMsg); ok {
				t.Fatalf("sentinelMsg forwarded after n; should only pop")
			}
		}
	}
}

// TestConfirmEscDeclinesPopsOnly verifies that Esc declines without forwarding.
func TestConfirmEscDeclinesPopsOnly(t *testing.T) {
	model := buildConfirmModel(t, "Confirm?", sentinelMsg{id: 2})

	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)

	if model.currentModal() != nil && model.currentModal().Name() == ModalConfirm {
		t.Fatal("confirm modal should be popped after Esc")
	}
	if cmd != nil {
		msgs := collectCommandMessagesForTest(t, cmd)
		for _, m := range msgs {
			if _, ok := m.(sentinelMsg); ok {
				t.Fatalf("sentinelMsg forwarded after Esc; should only pop")
			}
		}
	}
}

// TestConfirmCtrlCDeclinesPopsOnly verifies that ctrl+c declines without forwarding.
func TestConfirmCtrlCDeclinesPopsOnly(t *testing.T) {
	model := buildConfirmModel(t, "Confirm?", sentinelMsg{id: 3})

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = next.(Model)

	if model.currentModal() != nil && model.currentModal().Name() == ModalConfirm {
		t.Fatal("confirm modal should be popped after ctrl+c")
	}
	if cmd != nil {
		msgs := collectCommandMessagesForTest(t, cmd)
		for _, m := range msgs {
			if _, ok := m.(sentinelMsg); ok {
				t.Fatalf("sentinelMsg forwarded after ctrl+c; should only pop")
			}
		}
	}
}

// TestConfirmUnknownKeyIsNoop verifies that unrecognised keys are ignored.
func TestConfirmUnknownKeyIsNoop(t *testing.T) {
	model := buildConfirmModel(t, "Confirm?", sentinelMsg{id: 4})
	before := len(model.modals)

	model = pressKey(model, "x")

	if got := len(model.modals); got != before {
		t.Fatalf("modal stack len changed from %d to %d after unrecognised key", before, got)
	}
	if model.currentModal() == nil || model.currentModal().Name() != ModalConfirm {
		t.Fatal("confirm should still be the current modal after unrecognised key")
	}
}

// ---- Wizard + confirm integration tests ----

// TestWizardCtrlCPushesConfirm verifies that ctrl+c inside the wizard (any step)
// pushes the discard confirm instead of immediately dismissing.
func TestWizardCtrlCPushesConfirm(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())

	// ctrl+c at StepMode (the default step after wizard open).
	next, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = next.(Model)
	msgs := collectCommandMessagesForTest(t, cmd)
	for _, m := range msgs {
		next2, _ := model.Update(m)
		model = next2.(Model)
	}

	if model.currentModal() == nil || model.currentModal().Name() != ModalConfirm {
		t.Fatalf("currentModal = %v, want ModalConfirm after ctrl+c in wizard", model.currentModal())
	}
}

// TestWizardCtrlCAtDifferentStepPushesConfirm verifies ctrl+c works at non-Mode steps.
func TestWizardCtrlCAtDifferentStepPushesConfirm(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())
	model = pressKey(model, "enter") // → StepName

	w := currentWizard(t, model)
	if w.step != StepName {
		t.Fatalf("step = %q, want StepName before ctrl+c test", w.step)
	}

	next, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = next.(Model)
	msgs := collectCommandMessagesForTest(t, cmd)
	for _, m := range msgs {
		next2, _ := model.Update(m)
		model = next2.(Model)
	}

	if model.currentModal() == nil || model.currentModal().Name() != ModalConfirm {
		t.Fatalf("currentModal = %v, want ModalConfirm after ctrl+c at StepName", model.currentModal())
	}
}

// TestWizardDiscardConfirmYesDiscards drives the full "Esc → confirm yes → wizard gone" flow.
func TestWizardDiscardConfirmYesDiscards(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())

	// Push confirm via Esc at StepMode.
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)
	msgs := collectCommandMessagesForTest(t, cmd)
	for _, m := range msgs {
		next2, _ := model.Update(m)
		model = next2.(Model)
	}
	if model.currentModal() == nil || model.currentModal().Name() != ModalConfirm {
		t.Fatalf("expected ModalConfirm, got %v", model.currentModal())
	}

	// Confirm yes: pops confirm + pops wizard.
	model = pressKeyAndDriveCmds(t, model, "y")

	// Wizard should be gone; picker should be the current modal.
	for _, mod := range model.modals {
		if mod.Name() == ModalNewConnectionWizard {
			t.Fatal("wizard should be gone after confirm yes")
		}
	}
	if model.currentModal() == nil || model.currentModal().Name() != ModalConnectionPicker {
		t.Fatalf("currentModal = %v, want ModalConnectionPicker after discard confirmed", model.currentModal())
	}
}

// TestWizardDiscardConfirmNoResumesWizard drives "Esc → confirm no → wizard resumes".
func TestWizardDiscardConfirmNoResumesWizard(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())

	// Advance to StepName and type a partial name so we can verify state is intact.
	model = pressKey(model, "enter") // → StepName
	model = typeText(model, "partial")

	w := currentWizard(t, model)
	if w.step != StepName {
		t.Fatalf("step = %q, want StepName", w.step)
	}
	if w.name != "partial" {
		t.Fatalf("name = %q, want 'partial'", w.name)
	}

	// Push confirm via ctrl+c (works at any step).
	next, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = next.(Model)
	msgs := collectCommandMessagesForTest(t, cmd)
	for _, m := range msgs {
		next2, _ := model.Update(m)
		model = next2.(Model)
	}
	if model.currentModal() == nil || model.currentModal().Name() != ModalConfirm {
		t.Fatalf("expected ModalConfirm, got %v", model.currentModal())
	}

	// Confirm no: pops confirm, wizard resumes with state intact.
	model = pressKey(model, "n")

	if model.currentModal() == nil || model.currentModal().Name() != ModalNewConnectionWizard {
		t.Fatalf("currentModal = %v, want ModalNewConnectionWizard after confirm no", model.currentModal())
	}
	w = currentWizard(t, model)
	if w.step != StepName {
		t.Fatalf("step = %q, want StepName (state must be intact after no)", w.step)
	}
	if w.name != "partial" {
		t.Fatalf("name = %q, want 'partial' (in-progress name must be intact after no)", w.name)
	}
}

// TestWizardDiscardConfirmEscResumesWizard verifies Esc on the confirm also resumes.
func TestWizardDiscardConfirmEscResumesWizard(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())

	// Push confirm via Esc at StepMode.
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)
	msgs := collectCommandMessagesForTest(t, cmd)
	for _, m := range msgs {
		next2, _ := model.Update(m)
		model = next2.(Model)
	}
	if model.currentModal() == nil || model.currentModal().Name() != ModalConfirm {
		t.Fatalf("expected ModalConfirm, got %v", model.currentModal())
	}

	// Esc the confirm: wizard resumes at StepMode.
	model = pressKey(model, "esc")

	if model.currentModal() == nil || model.currentModal().Name() != ModalNewConnectionWizard {
		t.Fatalf("currentModal = %v, want ModalNewConnectionWizard after Esc on confirm", model.currentModal())
	}
	w := currentWizard(t, model)
	if w.step != StepMode {
		t.Fatalf("step = %q, want StepMode (wizard resumed where it was)", w.step)
	}
}

// TestWizardDiscardConfirmCtrlCOnConfirmResumesWizard verifies ctrl+c on the confirm
// dismisses only the confirm (does not quit the whole wizard).
func TestWizardDiscardConfirmCtrlCOnConfirmResumesWizard(t *testing.T) {
	model := buildWizardModel(t, config.Connections{}, t.TempDir())

	// Push confirm via Esc at StepMode.
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = next.(Model)
	msgs := collectCommandMessagesForTest(t, cmd)
	for _, m := range msgs {
		next2, _ := model.Update(m)
		model = next2.(Model)
	}
	if model.currentModal() == nil || model.currentModal().Name() != ModalConfirm {
		t.Fatalf("expected ModalConfirm, got %v", model.currentModal())
	}

	// ctrl+c on the confirm modal: pops confirm (no quit, no forward).
	model = pressKey(model, "ctrl+c")

	if model.currentModal() == nil || model.currentModal().Name() != ModalNewConnectionWizard {
		t.Fatalf("currentModal = %v, want ModalNewConnectionWizard after ctrl+c on confirm", model.currentModal())
	}
}
