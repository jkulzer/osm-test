package ui

import (
	"errors"
	"fmt"
	"image/color"

	fyne "fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/jkulzer/osm-test/helpers"
	"github.com/jkulzer/osm-test/models"

	"github.com/paulmach/osm"

	"github.com/rs/zerolog/log"
)

type PlatformSelectorWidget struct {
	widget.BaseWidget
	items              models.PlatformList
	sourcePlatform     osm.ElementID
	destPlatform       osm.ElementID
	SourcePlatformChan chan osm.ElementID
	DestPlatformChan   chan osm.ElementID
}

func NewPlatformSelector(items models.PlatformList) *PlatformSelectorWidget {
	w := &PlatformSelectorWidget{items: items}
	w.ExtendBaseWidget(w)
	return w
}

func (w *PlatformSelectorWidget) CreateRenderer() fyne.WidgetRenderer {
	content := container.NewVBox()
	content.Add(container.NewVBox(canvas.NewText("Service list:", color.White)))

	serviceToPlatform := make(map[*osm.Relation]osm.ElementID)

	for _, platform := range w.items.Platforms {
		for _, service := range platform.Services {
			serviceToPlatform[service] = platform.ElementID
		}
	}

	lightRailServices := make(map[*osm.Relation]osm.ElementID)
	subwayServices := make(map[*osm.Relation]osm.ElementID)
	tramServices := make(map[*osm.Relation]osm.ElementID)
	trolleyBusServices := make(map[*osm.Relation]osm.ElementID)
	busServices := make(map[*osm.Relation]osm.ElementID)
	ferryServices := make(map[*osm.Relation]osm.ElementID)
	remainingServices := make(map[*osm.Relation]osm.ElementID)

	for service, platform := range serviceToPlatform {
		switch service.Tags.Find("route") {
		case "light_rail":
			lightRailServices[service] = platform
		case "subway":
			subwayServices[service] = platform
		case "tram":
			tramServices[service] = platform
		case "trolleybus":
			trolleyBusServices[service] = platform
		case "bus":
			busServices[service] = platform
		case "ferry":
			ferryServices[service] = platform
		default:
			remainingServices[service] = platform
		}
	}

	if len(lightRailServices) != 0 {
		serviceHeadline := canvas.NewText("Light rail:", color.White)
		content.Add(serviceHeadline)
		for service, platformID := range lightRailServices {
			displayService(w, service, platformID, content)
		}
	}
	if len(subwayServices) != 0 {
		serviceHeadline := canvas.NewText("Subway:", color.White)
		content.Add(serviceHeadline)
		for service, platformID := range subwayServices {
			displayService(w, service, platformID, content)
		}
	}
	if len(tramServices) != 0 {
		serviceHeadline := canvas.NewText("Tram:", color.White)
		content.Add(serviceHeadline)
		for service, platformID := range tramServices {
			displayService(w, service, platformID, content)
		}
	}
	if len(trolleyBusServices) != 0 {
		serviceHeadline := canvas.NewText("Trolley Bus:", color.White)
		content.Add(serviceHeadline)
		for service, platformID := range trolleyBusServices {
			displayService(w, service, platformID, content)
		}
	}
	if len(busServices) != 0 {
		serviceHeadline := canvas.NewText("Bus:", color.White)
		content.Add(serviceHeadline)
		for service, platformID := range busServices {
			displayService(w, service, platformID, content)
		}
	}
	if len(ferryServices) != 0 {
		serviceHeadline := canvas.NewText("Ferry:", color.White)
		content.Add(serviceHeadline)
		for service, platformID := range ferryServices {
			displayService(w, service, platformID, content)
		}
	}
	if len(remainingServices) != 0 {
		serviceHeadline := canvas.NewText("Everything else:", color.White)
		content.Add(serviceHeadline)
		for service, platformID := range remainingServices {
			displayService(w, service, platformID, content)
		}
	}

	scroll := container.NewVScroll(content)
	return widget.NewSimpleRenderer(scroll)
}

func displayService(w *PlatformSelectorWidget, service *osm.Relation, platformID osm.ElementID, content *fyne.Container) {
	// platform details
	var platformNumber string
	switch platformID.Type() {
	case "way":
		platformData := w.items.Ways[platformID.WayID()]
		platformNumber = platformData.Tags.Find("ref")
	case "relation":
		platformData := w.items.Relations[platformID.RelationID()]
		platformNumber = platformData.Tags.Find("ref")
	default:
		log.Warn().Msg("Platform " + fmt.Sprint(platformID) + " is neither way nor relation")
	}

	// service details
	serviceColor := color.RGBA{255, 255, 255, 1}
	colorString := service.Tags.Find("colour")
	red, green, blue, err := helpers.ColorFromString(colorString)
	serviceColor.R = uint8(red)
	serviceColor.G = uint8(green)
	serviceColor.B = uint8(blue)
	serviceColor.A = uint8(255)

	// platform number logic
	var platformString string
	if platformNumber != "" {
		platformString = " on platform " + platformNumber
	} else {
		platformString = ""
	}

	sourceButton := widget.NewButton("Start here", func() {
		log.Info().Msg("source platform: " + fmt.Sprint(platformID))
		w.sourcePlatform = platformID
		if int64(w.destPlatform) != 0 {
			fmt.Println("done")
			w.SourcePlatformChan <- w.sourcePlatform
			w.DestPlatformChan <- w.destPlatform
		}
	})
	destButton := widget.NewButton("End here", func() {
		log.Info().Msg("dest platform: " + fmt.Sprint(platformID))
		w.destPlatform = platformID
		if int64(w.sourcePlatform) != 0 {
			fmt.Println("done")
			w.SourcePlatformChan <- w.sourcePlatform
			w.DestPlatformChan <- w.destPlatform
		}
	})

	var text *canvas.Text
	if err != nil {
		log.Warn().Msg("failed decoding color")
		text = canvas.NewText(service.Tags.Find("ref")+" to "+service.Tags.Find("to")+platformString, color.White)
	} else {
		text = canvas.NewText(service.Tags.Find("ref")+" to "+service.Tags.Find("to")+platformString, serviceColor)
	}
	serviceContainer := container.New(layout.NewHBoxLayout(), text, sourceButton, destButton)
	content.Add(serviceContainer)
}

func DisplayResults(ctx models.AppContext, alongSourcePlatform float64, alongDestPlatform float64) {

	sourcePlatformText := canvas.NewText(fmt.Sprint(alongSourcePlatform*100)+"% along source platform", color.White)
	destPlatformText := canvas.NewText(fmt.Sprint(alongDestPlatform*100)+"% along dest platform", color.White)
	serviceContainer := container.New(layout.NewVBoxLayout(), sourcePlatformText, destPlatformText)
	content := container.NewVBox()
	content.Add(serviceContainer)
	ctx.Tabs.Items[2].Content = content
}

func ShowFilePicker(w fyne.Window, reader chan (fyne.URIReadCloser), returnError chan (error)) {
	dialog.ShowError(errors.New("launching file picker"), w)
	filePicker := dialog.NewFileOpen(func(f fyne.URIReadCloser, err error) {
		reader <- f
		returnError <- err
	}, w)
	filePicker.Show()
}
