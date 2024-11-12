package models

import (
	"fyne.io/fyne/v2"
	"github.com/rs/zerolog"
)

type AppContext struct {
	Log    zerolog.Logger
	Window fyne.Window
}
