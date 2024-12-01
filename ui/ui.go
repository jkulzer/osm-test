package ui

import (
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

	"github.com/jkulzer/osm"

	"github.com/rs/zerolog/log"
)

type PlatformSelectorWidget struct {
	widget.BaseWidget
	items              models.PlatformList
	sourcePlatform     osm.ElementID
	destPlatform       osm.ElementID
	sourceService      osm.Relation
	destService        osm.Relation
	SourcePlatformChan chan models.PlatformAndServiceSelection
	DestPlatformChan   chan models.PlatformAndServiceSelection
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
	platformType, err := platformID.Type()
	if err != nil {
		log.Err(err).Msg("invalid element type detected")
	}
	switch platformType {
	case "way":
		wayID, err := platformID.WayID()
		if err != nil {
			log.Err(err).Msg("determining WayID of platform " + fmt.Sprint(platformID) + " failed since it is not of type way")
		}
		platformData := w.items.Ways[wayID]
		platformNumber = platformData.Tags.Find("ref")
	case "relation":
		relationID, err := platformID.RelationID()
		if err != nil {
			log.Err(err).Msg("determining RelationID of platform " + fmt.Sprint(platformID) + " failed since it is not of type relation")
		}
		platformData := w.items.Relations[relationID]
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
		w.sourceService = *service
		if int64(w.destPlatform) != 0 {
			fmt.Println("done")
			w.SourcePlatformChan <- models.PlatformAndServiceSelection{
				Platform: w.sourcePlatform,
				Service:  w.sourceService.ID,
			}
			w.DestPlatformChan <- models.PlatformAndServiceSelection{
				Platform: w.destPlatform,
				Service:  w.destService.ID,
			}
		}
	})
	destButton := widget.NewButton("End here", func() {
		log.Info().Msg("dest platform: " + fmt.Sprint(platformID))
		w.destPlatform = platformID
		w.destService = *service
		if int64(w.sourcePlatform) != 0 {
			fmt.Println("done")
			w.SourcePlatformChan <- models.PlatformAndServiceSelection{
				Platform: w.sourcePlatform,
				Service:  w.sourceService.ID,
			}
			w.DestPlatformChan <- models.PlatformAndServiceSelection{
				Platform: w.destPlatform,
				Service:  w.destService.ID,
			}
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

func DisplayResults(ctx models.AppContext, alongSourcePlatform float64, alongDestPlatform float64, fromPlatformStart float64, toPlatformStart float64) {

	sourcePlatformText := canvas.NewText(fmt.Sprint(alongSourcePlatform*100)+"% along source platform or "+fmt.Sprint(fromPlatformStart)+"m", color.White)
	destPlatformText := canvas.NewText(fmt.Sprint(alongDestPlatform*100)+"% along dest platform or "+fmt.Sprint(toPlatformStart)+"m", color.White)
	serviceContainer := container.New(layout.NewVBoxLayout(), sourcePlatformText, destPlatformText)
	content := container.NewVBox()
	content.Add(serviceContainer)
	ctx.Tabs.Items[2].Content = content
}

func ShowFilePicker(w fyne.Window, reader chan (fyne.URIReadCloser), returnError chan (error)) {
	filePicker := dialog.NewFileOpen(func(f fyne.URIReadCloser, err error) {
		go func() {
			reader <- f
			returnError <- err
		}()
	}, w)
	filePicker.Show()
}

func ShowPlatformEdgeSelector(w fyne.Window, platformEdges []*osm.Way) {
	var customDialog *dialog.CustomDialog
	content := container.NewVBox()
	for edgeIndex, edge := range platformEdges {
		edgePlatformNumber := edge.Tags.Find("ref")
		edgeEntry := widget.NewButton(edgePlatformNumber, func() {
			log.Info().Msg("selected platform edge " + edgePlatformNumber + " with slice index " + fmt.Sprint(edgeIndex))
			customDialog.Hide()
		})
		content.Add(edgeEntry)
	}
	customDialog = dialog.NewCustomWithoutButtons("Select platform edge:", content, w)
	customDialog.Show()
}
