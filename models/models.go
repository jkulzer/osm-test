package models

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"github.com/fatih/color"
	"github.com/rs/zerolog"

	"github.com/paulmach/orb"

	"github.com/paulmach/osm"
)

type AppContext struct {
	Log    zerolog.Logger
	Window fyne.Window
	Tabs   *container.AppTabs
}

type PlatformItem struct {
	ElementID osm.ElementID
	Services  []*osm.Relation
}

type PlatformAndServiceSelection struct {
	Platform osm.ElementID
	Service  osm.RelationID
}

type PlatformList struct {
	Platforms []PlatformItem
	Ways      map[osm.WayID]*osm.Way
	Relations map[osm.RelationID]*osm.Relation
	// SourcePlatform chan osm.ElementID
	// DestPlatform   chan osm.ElementID
}

type Service struct {
	Name        string
	Operator    string
	Origin      string
	Destination string
	Color       color.Color
}

type PlatformSpine struct {
	Start orb.Point
	End   orb.Point
}
