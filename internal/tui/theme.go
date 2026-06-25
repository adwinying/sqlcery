package tui

import "github.com/charmbracelet/lipgloss"

var AppTheme = newTUITheme()

type tuiTheme struct {
	AppTitle                 lipgloss.Style
	MetaLine                 lipgloss.Style
	StatusBar                lipgloss.Style
	PaneBorderActive         lipgloss.Style
	PaneBorderInactive       lipgloss.Style
	ModalBorder              lipgloss.Style
	SectionHeading           lipgloss.Style
	ActiveSectionHeading     lipgloss.Style
	Separator                lipgloss.Style
	PanelTitle               lipgloss.Style
	PanelText                lipgloss.Style
	PanelMuted               lipgloss.Style
	PanelSelected            lipgloss.Style
	PanelHint                lipgloss.Style
	InfoNotice               lipgloss.Style
	WarningNotice            lipgloss.Style
	ErrorNotice              lipgloss.Style
	NotificationSuccess      lipgloss.Style
	NotificationInfo         lipgloss.Style
	NotificationError        lipgloss.Style
	ResultTitle              lipgloss.Style
	ResultHeader             lipgloss.Style
	ResultSeparator          lipgloss.Style
	ResultSummary            lipgloss.Style
	ResultsPaneTitle         lipgloss.Style
	ResultsPaneMeta          lipgloss.Style
	ResultsPaneSelection     lipgloss.Style
	ResultsPaneEmpty         lipgloss.Style
	ResultsPaneEmptyLogo     lipgloss.Style
	ResultsPaneEmptySubtitle lipgloss.Style
	ResultsActiveRow         lipgloss.Style
	ResultsMarkedRow         lipgloss.Style
	ResultsMarkedActiveRow   lipgloss.Style
	KeywordStyle             lipgloss.Style
	StringStyle              lipgloss.Style
	NumberStyle              lipgloss.Style
	CommentStyle             lipgloss.Style
	QuotedIdentifierStyle    lipgloss.Style
	ParameterStyle           lipgloss.Style
	OperatorStyle            lipgloss.Style
	PromptStyle              lipgloss.Style
	LineNumberStyle          lipgloss.Style
	CursorLineNumberStyle    lipgloss.Style
	PlaceholderStyle         lipgloss.Style
	CursorLineStyle          lipgloss.Style
	CursorStyle              lipgloss.Style
	GhostTextStyle           lipgloss.Style
}

func newTUITheme() tuiTheme {
	accent := lipgloss.Color("12")
	accentSoft := lipgloss.Color("6")
	accentWarm := lipgloss.Color("3")
	success := lipgloss.Color("2")
	danger := lipgloss.Color("1")
	muted := lipgloss.Color("8")
	selectedForeground := lipgloss.Color("15")
	selectedBackground := lipgloss.Color("4")
	return tuiTheme{
		AppTitle:                 lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		MetaLine:                 lipgloss.NewStyle(),
		StatusBar:                lipgloss.NewStyle(),
		PaneBorderActive:         lipgloss.NewStyle().Foreground(accent),
		PaneBorderInactive:       lipgloss.NewStyle().Foreground(muted),
		ModalBorder:              lipgloss.NewStyle().Foreground(accent),
		SectionHeading:           lipgloss.NewStyle().Foreground(accentSoft).Bold(true),
		ActiveSectionHeading:     lipgloss.NewStyle().Foreground(accentWarm).Bold(true).Underline(true),
		Separator:                lipgloss.NewStyle().Foreground(muted),
		PanelTitle:               lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		PanelText:                lipgloss.NewStyle(),
		PanelMuted:               lipgloss.NewStyle().Foreground(muted),
		PanelSelected:            lipgloss.NewStyle().Foreground(selectedForeground).Background(selectedBackground).Bold(true),
		PanelHint:                lipgloss.NewStyle().Foreground(muted).Italic(true),
		InfoNotice:               lipgloss.NewStyle().Foreground(accentSoft).Bold(true),
		WarningNotice:            lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		ErrorNotice:              lipgloss.NewStyle().Foreground(danger).Bold(true),
		NotificationSuccess:      lipgloss.NewStyle().Foreground(success).Bold(true),
		NotificationInfo:         lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		NotificationError:        lipgloss.NewStyle().Foreground(danger).Bold(true),
		ResultTitle:              lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		ResultHeader:             lipgloss.NewStyle().Foreground(accentSoft).Bold(true),
		ResultSeparator:          lipgloss.NewStyle().Foreground(muted),
		ResultSummary:            lipgloss.NewStyle().Foreground(success),
		ResultsPaneTitle:         lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		ResultsPaneMeta:          lipgloss.NewStyle(),
		ResultsPaneSelection:     lipgloss.NewStyle().Foreground(accentSoft).Bold(true),
		ResultsPaneEmpty:         lipgloss.NewStyle().Foreground(muted),
		ResultsPaneEmptyLogo:     lipgloss.NewStyle().Bold(true),
		ResultsPaneEmptySubtitle: lipgloss.NewStyle().Foreground(accentSoft),
		ResultsActiveRow:         lipgloss.NewStyle().Foreground(selectedForeground).Background(selectedBackground),
		ResultsMarkedRow:         lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		ResultsMarkedActiveRow:   lipgloss.NewStyle().Foreground(accentWarm).Background(selectedBackground).Bold(true),
		KeywordStyle:             lipgloss.NewStyle().Foreground(accent).Bold(true),
		StringStyle:              lipgloss.NewStyle().Foreground(success),
		NumberStyle:              lipgloss.NewStyle().Foreground(accentSoft),
		CommentStyle:             lipgloss.NewStyle().Foreground(muted).Italic(true),
		QuotedIdentifierStyle:    lipgloss.NewStyle().Foreground(accentSoft),
		ParameterStyle:           lipgloss.NewStyle().Foreground(accentWarm),
		OperatorStyle:            lipgloss.NewStyle().Foreground(accentWarm),
		PromptStyle:              lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		LineNumberStyle:          lipgloss.NewStyle().Foreground(muted),
		CursorLineNumberStyle:    lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		PlaceholderStyle:         lipgloss.NewStyle().Foreground(muted).Italic(true),
		CursorLineStyle:          lipgloss.NewStyle(),
		CursorStyle:              lipgloss.NewStyle().Reverse(true),
		GhostTextStyle:           lipgloss.NewStyle().Foreground(muted).Italic(true),
	}
}
