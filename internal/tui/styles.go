package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/coolxitech/nali/pkg/entity"
)

// styles holds every lipgloss style used to render the interface. The entity
// colours match the ones the plain CLI output uses: IPv4 green, IPv6 blue,
// domain yellow and plain text unstyled.
type styles struct {
	title       lipgloss.Style
	version     lipgloss.Style
	statusOK    lipgloss.Style
	statusBad   lipgloss.Style
	note        lipgloss.Style
	prompt      lipgloss.Style
	placeholder lipgloss.Style
	index       lipgloss.Style
	kind        lipgloss.Style
	source      lipgloss.Style
	answer      lipgloss.Style
	failure     lipgloss.Style
	hint        lipgloss.Style
	bar         lipgloss.Style
	barEmpty    lipgloss.Style
	ipv4        lipgloss.Style
	ipv6        lipgloss.Style
	domain      lipgloss.Style
	plain       lipgloss.Style
}

// newStyles builds the style set. Only the basic ANSI colours are used so that
// the TUI looks like the rest of the tool on every terminal.
func newStyles() styles {
	return styles{
		title:       lipgloss.NewStyle().Bold(true),
		version:     lipgloss.NewStyle().Bold(true),
		statusOK:    lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		statusBad:   lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		note:        lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		prompt:      lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		placeholder: lipgloss.NewStyle().Faint(true),
		index:       lipgloss.NewStyle().Faint(true),
		kind:        lipgloss.NewStyle().Faint(true),
		source:      lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		answer:      lipgloss.NewStyle(),
		failure:     lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		hint:        lipgloss.NewStyle().Faint(true),
		bar:         lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		barEmpty:    lipgloss.NewStyle().Faint(true),
		ipv4:        lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		ipv6:        lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		domain:      lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		plain:       lipgloss.NewStyle(),
	}
}

// entityStyle returns the style of one entity type.
func (s styles) entityStyle(t entity.EntityType) lipgloss.Style {
	switch t {
	case entity.TypeIPv4:
		return s.ipv4
	case entity.TypeIPv6:
		return s.ipv6
	case entity.TypeDomain:
		return s.domain
	default:
		return s.plain
	}
}
