package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	Background              string `toml:"background"`
	MarkerBackground        string `toml:"marker_background"`
	MarkerInsertBackground  string `toml:"marker_insert_background"`
	MarkerReplaceBackground string `toml:"marker_replace_background"`
	IndexMarkerBackground   string `toml:"index_marker_background"`
	LegendBackground        string `toml:"legend_background"`
	LegendHighlight         string `toml:"legend_highlight"`
	BorderColor             string `toml:"border_color"`
	EndianColor             string `toml:"endian_color"`
	ActiveTab               string `toml:"active_tab"`
	SelectionBackground     string `toml:"selection_background"`
	UnsavedFileColor        string `toml:"unsaved_file_color"`
	DisabledColor           string `toml:"disabled_color"`
	Bit16Background         string `toml:"bit16_background"`
	Bit32Background         string `toml:"bit32_background"`
	Bit64Background         string `toml:"bit64_background"`
	Bit128Background        string `toml:"bit128_background"`
}

type Config struct {
	Theme Theme `toml:"theme"`
}

func DefaultConfig() *Config {
	return &Config{
		Theme: Theme{
			Background:              "#000000",
			MarkerBackground:        "#0000FF",
			MarkerInsertBackground:  "#FF0000",
			MarkerReplaceBackground: "#FFFF00",
			IndexMarkerBackground:   "#000080",
			LegendBackground:        "#0000FF",
			LegendHighlight:         "#FF0000",
			BorderColor:             "#0000FF",
			EndianColor:             "#333333",
			ActiveTab:               "#FF00FF",
			SelectionBackground:     "#FFAA00",
			UnsavedFileColor:        "#FF0000",
			DisabledColor:           "#666666",
			Bit16Background:         "#004400",
			Bit32Background:         "#440044",
			Bit64Background:         "#004444",
			Bit128Background:        "#444400",
		},
	}
}

func ConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "unhexed.toml"
	}
	return filepath.Join(home, ".config", "unhexed", "unhexed.toml")
}

func Load() (*Config, error) {
	cfg := DefaultConfig()
	path := ConfigPath()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func (c *Config) Save() error {
	path := ConfigPath()
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return toml.NewEncoder(f).Encode(c)
}

type Styles struct {
	Background      lipgloss.Style
	MarkerNormal    lipgloss.Style
	MarkerInsert    lipgloss.Style
	MarkerReplace   lipgloss.Style
	IndexMarker     lipgloss.Style
	Legend          lipgloss.Style
	LegendHighlight lipgloss.Style
	Border          lipgloss.Style
	Endian          lipgloss.Style
	ActiveTab       lipgloss.Style
	InactiveTab     lipgloss.Style
	Selection       lipgloss.Style
	UnsavedFile     lipgloss.Style
	Disabled        lipgloss.Style
	Normal          lipgloss.Style
	DecoderLabel    lipgloss.Style
	DecoderValue    lipgloss.Style
	HelpTitle       lipgloss.Style
	HelpKey         lipgloss.Style
	HelpDesc        lipgloss.Style
	Bit16           lipgloss.Style
	Bit32           lipgloss.Style
	Bit64           lipgloss.Style
	Bit128          lipgloss.Style
}

func NewStyles(theme *Theme) *Styles {
	return &Styles{
		Background: lipgloss.NewStyle().
			Background(lipgloss.Color(theme.Background)),
		MarkerNormal: lipgloss.NewStyle().
			Background(lipgloss.Color(theme.MarkerBackground)).
			Foreground(lipgloss.Color("#FFFFFF")),
		MarkerInsert: lipgloss.NewStyle().
			Background(lipgloss.Color(theme.MarkerInsertBackground)).
			Foreground(lipgloss.Color("#FFFFFF")),
		MarkerReplace: lipgloss.NewStyle().
			Background(lipgloss.Color(theme.MarkerReplaceBackground)).
			Foreground(lipgloss.Color("#000000")),
		IndexMarker: lipgloss.NewStyle().
			Background(lipgloss.Color(theme.IndexMarkerBackground)).
			Foreground(lipgloss.Color("#FFFFFF")),
		Legend: lipgloss.NewStyle().
			Background(lipgloss.Color(theme.LegendBackground)).
			Foreground(lipgloss.Color("#FFFFFF")),
		LegendHighlight: lipgloss.NewStyle().
			Background(lipgloss.Color(theme.LegendBackground)).
			Foreground(lipgloss.Color(theme.LegendHighlight)).
			Bold(true),
		Border: lipgloss.NewStyle().
			BorderForeground(lipgloss.Color(theme.BorderColor)),
		Endian: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.EndianColor)),
		ActiveTab: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ActiveTab)).
			Bold(true),
		InactiveTab: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA")),
		Selection: lipgloss.NewStyle().
			Background(lipgloss.Color(theme.SelectionBackground)).
			Foreground(lipgloss.Color("#000000")),
		UnsavedFile: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.UnsavedFileColor)),
		Disabled: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.DisabledColor)),
		Normal: lipgloss.NewStyle(),
		DecoderLabel: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")),
		DecoderValue: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")),
		HelpTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")),
		HelpKey: lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.LegendHighlight)).
			Bold(true),
		HelpDesc: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA")),
		Bit16: lipgloss.NewStyle().
			Background(lipgloss.Color(theme.Bit16Background)).
			Foreground(lipgloss.Color("#FFFFFF")),
		Bit32: lipgloss.NewStyle().
			Background(lipgloss.Color(theme.Bit32Background)).
			Foreground(lipgloss.Color("#FFFFFF")),
		Bit64: lipgloss.NewStyle().
			Background(lipgloss.Color(theme.Bit64Background)).
			Foreground(lipgloss.Color("#FFFFFF")),
		Bit128: lipgloss.NewStyle().
			Background(lipgloss.Color(theme.Bit128Background)).
			Foreground(lipgloss.Color("#FFFFFF")),
	}
}
