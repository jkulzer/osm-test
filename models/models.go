package models

import (
	"fyne.io/fyne/v2"
	"github.com/fatih/color"
	"github.com/rs/zerolog"

	"github.com/paulmach/osm"
)

type AppContext struct {
	Log    zerolog.Logger
	Window fyne.Window
}

type PlatformItem struct {
	ElementID      osm.ElementID
	PlatformNumber string
	Services       []*osm.Relation
}

type PlatformList []PlatformItem

type Service struct {
	Name        string
	Operator    string
	Origin      string
	Destination string
	Color       color.Color
}
