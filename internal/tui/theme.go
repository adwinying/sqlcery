package tui

import "github.com/charmbracelet/lipgloss"

var AppTheme = newTUITheme()

type tuiTheme struct {
	AppTitle                 lipgloss.Style
	MetaLine                 lipgloss.Style
	Footer                   lipgloss.Style
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
	accent := lipgloss.AdaptiveColor{Light: "25", Dark: "117"}
	accentSoft := lipgloss.AdaptiveColor{Light: "31", Dark: "111"}
	accentWarm := lipgloss.AdaptiveColor{Light: "130", Dark: "221"}
	success := lipgloss.AdaptiveColor{Light: "28", Dark: "114"}
	danger := lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	muted := lipgloss.AdaptiveColor{Light: "240", Dark: "246"}
	mutedSoft := lipgloss.AdaptiveColor{Light: "245", Dark: "243"}
	panelForeground := lipgloss.AdaptiveColor{Light: "237", Dark: "252"}
	footerForeground := lipgloss.AdaptiveColor{Light: "238", Dark: "252"}
	selectedForeground := lipgloss.AdaptiveColor{Light: "17", Dark: "231"}
	selectedBackground := lipgloss.AdaptiveColor{Light: "153", Dark: "24"}
	return tuiTheme{
		AppTitle:                 lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		MetaLine:                 lipgloss.NewStyle().Foreground(panelForeground),
		Footer:                   lipgloss.NewStyle().Foreground(footerForeground),
		PaneBorderActive:         lipgloss.NewStyle().Foreground(accent),
		PaneBorderInactive:       lipgloss.NewStyle().Foreground(mutedSoft),
		ModalBorder:              lipgloss.NewStyle().Foreground(accent),
		SectionHeading:           lipgloss.NewStyle().Foreground(accentSoft).Bold(true),
		ActiveSectionHeading:     lipgloss.NewStyle().Foreground(accentWarm).Bold(true).Underline(true),
		Separator:                lipgloss.NewStyle().Foreground(mutedSoft),
		PanelTitle:               lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		PanelText:                lipgloss.NewStyle().Foreground(panelForeground),
		PanelMuted:               lipgloss.NewStyle().Foreground(muted),
		PanelSelected:            lipgloss.NewStyle().Foreground(selectedForeground).Background(selectedBackground).Bold(true),
		PanelHint:                lipgloss.NewStyle().Foreground(mutedSoft).Italic(true),
		InfoNotice:               lipgloss.NewStyle().Foreground(accentSoft).Bold(true),
		WarningNotice:            lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		ErrorNotice:              lipgloss.NewStyle().Foreground(danger).Bold(true),
		NotificationSuccess:      lipgloss.NewStyle().Foreground(success).Bold(true),
		NotificationInfo:         lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		NotificationError:        lipgloss.NewStyle().Foreground(danger).Bold(true),
		ResultTitle:              lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		ResultHeader:             lipgloss.NewStyle().Foreground(accentSoft).Bold(true),
		ResultSeparator:          lipgloss.NewStyle().Foreground(mutedSoft),
		ResultSummary:            lipgloss.NewStyle().Foreground(success),
		ResultsPaneTitle:         lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		ResultsPaneMeta:          lipgloss.NewStyle().Foreground(panelForeground),
		ResultsPaneSelection:     lipgloss.NewStyle().Foreground(accentSoft).Bold(true),
		ResultsPaneEmpty:         lipgloss.NewStyle().Foreground(muted).Italic(true),
		ResultsPaneEmptyLogo:     lipgloss.NewStyle().Foreground(panelForeground).Bold(true),
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
		PlaceholderStyle:         lipgloss.NewStyle().Foreground(mutedSoft).Italic(true),
		CursorLineStyle:          lipgloss.NewStyle(),
		CursorStyle:              lipgloss.NewStyle().Reverse(true),
		GhostTextStyle:           lipgloss.NewStyle().Foreground(mutedSoft).Italic(true),
	}
}
