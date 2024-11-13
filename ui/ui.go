package ui

import (
	"fmt"
	"image/color"

	fyne "fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/jkulzer/osm-test/helpers"
	"github.com/jkulzer/osm-test/models"

	"github.com/paulmach/osm"

	"github.com/rs/zerolog/log"
)

type PlatformSelectorWidget struct {
	widget.BaseWidget
	items models.PlatformList
}

func NewPlatformSelector(items models.PlatformList) *PlatformSelectorWidget {
	w := &PlatformSelectorWidget{items: items}
	w.ExtendBaseWidget(w)
	return w
}

func (w *PlatformSelectorWidget) CreateRenderer() fyne.WidgetRenderer {
	content := container.NewVBox()
	content.Add(container.NewVBox(canvas.NewText("Platform list:", color.White)))
	for _, item := range w.items {
		// platform :=
		platformList := widget.NewLabel(fmt.Sprintf("Platform with ID " + fmt.Sprint(item.ElementID) + " and type " + fmt.Sprint(item.ElementID.Type())))

		serviceList := container.NewVBox()
		for _, service := range item.Services {
			serviceColor := color.RGBA{255, 255, 255, 1}
			colorString := service.Tags.Find("colour")
			red, green, blue, err := helpers.ColorFromString(colorString)
			serviceColor.R = uint8(red)
			serviceColor.G = uint8(green)
			serviceColor.B = uint8(blue)
			serviceColor.A = uint8(255)
			var text *canvas.Text
			if err != nil {
				log.Warn().Msg("failed decoding color")
				text = canvas.NewText("- "+fmt.Sprint(service.Tags.Find("name")), color.White)
			} else {
				text = canvas.NewText("- "+fmt.Sprint(service.Tags.Find("name")), serviceColor)
			}
			serviceList.Add(text)
		}
		if len(item.Services) != 0 {
			content.Add(container.NewVBox(platformList, serviceList))
		}
	}

	scroll := container.NewVScroll(content)
	return widget.NewSimpleRenderer(scroll)
}
