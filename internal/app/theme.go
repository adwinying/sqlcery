package app

import "github.com/charmbracelet/lipgloss"

var appTheme = newTUITheme()

type tuiTheme struct {
	appTitle              lipgloss.Style
	metaLine              lipgloss.Style
	footer                lipgloss.Style
	paneBorderActive      lipgloss.Style
	paneBorderInactive    lipgloss.Style
	sectionHeading        lipgloss.Style
	activeSectionHeading  lipgloss.Style
	separator             lipgloss.Style
	panelTitle            lipgloss.Style
	panelText             lipgloss.Style
	panelMuted            lipgloss.Style
	panelSelected         lipgloss.Style
	panelHint             lipgloss.Style
	infoNotice            lipgloss.Style
	warningNotice         lipgloss.Style
	errorNotice           lipgloss.Style
	resultTitle           lipgloss.Style
	resultHeader          lipgloss.Style
	resultSeparator       lipgloss.Style
	resultSummary         lipgloss.Style
	viewerTitle           lipgloss.Style
	viewerMeta            lipgloss.Style
	viewerSelection       lipgloss.Style
	viewerEmpty           lipgloss.Style
	viewerEmptyLogo       lipgloss.Style
	viewerEmptySubtitle   lipgloss.Style
	primaryKeyHeader      lipgloss.Style
	primaryKeyValue       lipgloss.Style
	selectedRowMarker     lipgloss.Style
	keywordStyle          lipgloss.Style
	stringStyle           lipgloss.Style
	numberStyle           lipgloss.Style
	commentStyle          lipgloss.Style
	quotedIdentifierStyle lipgloss.Style
	parameterStyle        lipgloss.Style
	operatorStyle         lipgloss.Style
	promptStyle           lipgloss.Style
	lineNumberStyle       lipgloss.Style
	cursorLineNumberStyle lipgloss.Style
	placeholderStyle      lipgloss.Style
	cursorLineStyle       lipgloss.Style
	cursorStyle           lipgloss.Style
	ghostTextStyle        lipgloss.Style
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
	footerBackground := lipgloss.AdaptiveColor{Light: "254", Dark: "236"}
	selectedForeground := lipgloss.AdaptiveColor{Light: "17", Dark: "231"}
	selectedBackground := lipgloss.AdaptiveColor{Light: "153", Dark: "24"}
	cursorLineBackground := lipgloss.AdaptiveColor{Light: "255", Dark: "236"}

	return tuiTheme{
		appTitle:              lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		metaLine:              lipgloss.NewStyle().Foreground(panelForeground),
		footer:                lipgloss.NewStyle().Foreground(footerForeground).Background(footerBackground),
		paneBorderActive:      lipgloss.NewStyle().Foreground(accent),
		paneBorderInactive:    lipgloss.NewStyle().Foreground(mutedSoft),
		sectionHeading:        lipgloss.NewStyle().Foreground(accentSoft).Bold(true),
		activeSectionHeading:  lipgloss.NewStyle().Foreground(accentWarm).Bold(true).Underline(true),
		separator:             lipgloss.NewStyle().Foreground(mutedSoft),
		panelTitle:            lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		panelText:             lipgloss.NewStyle().Foreground(panelForeground),
		panelMuted:            lipgloss.NewStyle().Foreground(muted),
		panelSelected:         lipgloss.NewStyle().Foreground(selectedForeground).Background(selectedBackground).Bold(true),
		panelHint:             lipgloss.NewStyle().Foreground(mutedSoft).Italic(true),
		infoNotice:            lipgloss.NewStyle().Foreground(accentSoft).Bold(true),
		warningNotice:         lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		errorNotice:           lipgloss.NewStyle().Foreground(danger).Bold(true),
		resultTitle:           lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		resultHeader:          lipgloss.NewStyle().Foreground(accentSoft).Bold(true),
		resultSeparator:       lipgloss.NewStyle().Foreground(mutedSoft),
		resultSummary:         lipgloss.NewStyle().Foreground(success),
		viewerTitle:           lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		viewerMeta:            lipgloss.NewStyle().Foreground(panelForeground),
		viewerSelection:       lipgloss.NewStyle().Foreground(accentSoft).Bold(true),
		viewerEmpty:           lipgloss.NewStyle().Foreground(muted).Italic(true),
		viewerEmptyLogo:       lipgloss.NewStyle().Foreground(panelForeground).Bold(true),
		viewerEmptySubtitle:   lipgloss.NewStyle().Foreground(muted).Italic(true),
		primaryKeyHeader:      lipgloss.NewStyle().Foreground(accentWarm).Bold(true).Underline(true),
		primaryKeyValue:       lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		selectedRowMarker:     lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		keywordStyle:          lipgloss.NewStyle().Foreground(accent).Bold(true),
		stringStyle:           lipgloss.NewStyle().Foreground(success),
		numberStyle:           lipgloss.NewStyle().Foreground(accentSoft),
		commentStyle:          lipgloss.NewStyle().Foreground(muted).Italic(true),
		quotedIdentifierStyle: lipgloss.NewStyle().Foreground(accentSoft),
		parameterStyle:        lipgloss.NewStyle().Foreground(accentWarm),
		operatorStyle:         lipgloss.NewStyle().Foreground(accentWarm),
		promptStyle:           lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		lineNumberStyle:       lipgloss.NewStyle().Foreground(muted),
		cursorLineNumberStyle: lipgloss.NewStyle().Foreground(accentWarm).Bold(true),
		placeholderStyle:      lipgloss.NewStyle().Foreground(mutedSoft).Italic(true),
		cursorLineStyle:       lipgloss.NewStyle().Background(cursorLineBackground),
		cursorStyle:           lipgloss.NewStyle().Reverse(true),
		ghostTextStyle:        lipgloss.NewStyle().Foreground(mutedSoft).Italic(true),
	}
}
