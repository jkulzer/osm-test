package main

import (
	"github.com/golang/geo/s2"
	"io"

	"encoding/json"
	"errors"

	"context"
	"fmt"
	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/path"
	"gonum.org/v1/gonum/graph/simple"
	"os"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/jkulzer/osm-test/helpers"
	"github.com/jkulzer/osm-test/linebound"
	"github.com/jkulzer/osm-test/models"
	"github.com/jkulzer/osm-test/ui"

	// logging
	"github.com/rs/zerolog/log"

	mapset "github.com/deckarep/golang-set/v2"

	"github.com/fatih/color"

	"github.com/jkulzer/osm"
	"github.com/jkulzer/osm/osmpbf"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geo"

	fyne "fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	// "fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
)

type GenericPlatform struct {
	Services mapset.Set[*osm.Relation]
}

type PlatformKey struct {
	Type osm.Type
	ID   int64
}

type WayPlatform struct {
}

func isStopPosition(tags osm.Tags, name string) bool {
	return tags.Find("public_transport") == "stop_position" && strings.Contains(tags.Find("name"), name)
}

// Check if a stop position has a route
func isPartOfRoute(tags osm.Tags) bool {
	return tags.Find("public_transport") == "stop_position" && tags.Find("route") != ""
}

func initAppContext() models.AppContext {
	return models.AppContext{}
}

func main() {
	fmt.Println("Data from:")
	fmt.Println("© OpenStreetMap contributors: https://openstreetmap.org/copyright")

	ctx := initAppContext()

	cpuProfile, _ := os.Create("cpuprofile")
	pprof.StartCPUProfile(cpuProfile)

	a := app.NewWithID("1")
	w := a.NewWindow("Platform Routing App")
	ctx.Window = w

	searchTermEntry := widget.NewEntry()
	searchTermEntry.SetPlaceHolder("Enter station name, e.g., 'Warschauer Straße'")

	// file, err := os.Open("berlin-latest.osm.pbf")

	progressBarOsmParsing := widget.NewProgressBar()

	var nodes map[osm.NodeID]*osm.Node
	var ways map[osm.WayID]*osm.Way
	var relations map[osm.RelationID]*osm.Relation
	footWays := mapset.NewSet[osm.NodeID]()
	var graph *simple.WeightedDirectedGraph

	startingProcessing := make(chan bool)
	doneProcessing := make(chan bool)

	// UI setup
	loadButton := container.NewVBox(progressBarOsmParsing)

	readerChan := make(chan fyne.URIReadCloser)
	searchProcessingReader := make(chan fyne.URIReadCloser)
	errChan := make(chan error)

	loadFileButton := container.NewVBox(widget.NewButton("Load Data", func() {
		go func() {
			ui.ShowFilePicker(w, readerChan, errChan)
		}()
	}))
	// Layout the UI components
	mainMenu := container.NewVBox(
		widget.NewLabel("Platform Routing Application"),
		searchTermEntry,
		loadButton,
		loadFileButton,
	)
	tabsList := []*container.TabItem{
		// don't forget t o add it to the container.NewAppTabs function below!!
		container.NewTabItem("Station search", mainMenu),
		container.NewTabItem("Service select", container.NewVBox()),
		container.NewTabItem("Result", container.NewVBox()),
	}
	tabsInstance := container.NewAppTabs(tabsList[0], tabsList[1], tabsList[2])
	tabsInstance.SetTabLocation(container.TabLocationBottom)

	ctx.Tabs = tabsInstance
	ctx.Tabs.DisableIndex(1)
	ctx.Tabs.DisableIndex(2)

	w.SetContent(tabsInstance)

	go func() {
		// uri, err := storage.ParseURI("file://berlin-latest.osm.pbf")
		// if err != nil {
		// 	log.Error().Err(err).Msg("Failed to parse uri")
		// 	dialog.ShowError(err, w)
		// }

		reader := <-readerChan
		log.Debug().Msg("received file reader for initial processing (1/2)")
		err := <-errChan
		if err != nil {
			log.Error().Err(err).Msg("Failed to open OSM file")
			dialog.ShowError(err, w)
		}
		startingProcessing <- true
		nodes, ways, relations, footWays, graph = processData(reader, ctx)
		log.Debug().Msg("initial processing done")
		doneProcessing <- true
		searchProcessingReader <- reader
	}()

	go func() {
		_ = <-startingProcessing
		for i := 0.0; i <= 0.95; i += 0.01 {
			time.Sleep(time.Millisecond * 100)
			progressBarOsmParsing.SetValue(i)
		}

		// after data parsing is done
		_ = <-doneProcessing
		progressBarOsmParsing.SetValue(1)

		button := container.NewVBox(widget.NewButton("Process Input File", func() {
			log.Info().Msg("started processing input data")
			ctx.Tabs.EnableIndex(1)
			ctx.Tabs.SelectIndex(1)
			// Call data parsing function
			go func() {
				reader := <-searchProcessingReader
				log.Debug().Msg("received file reader for further processing (2/2)")
				servicesAndPlatforms(reader, ctx, nodes, ways, relations, footWays, graph, searchTermEntry.Text)
			}()
		}))

		viewport1 := container.NewVBox(
			widget.NewLabel("Platform Routing Application"),
			searchTermEntry,
			button,
			// loadFileButton,
		)
		ctx.Tabs.Items[0].Content = viewport1
	}()
	w.ShowAndRun()

	pprof.StopCPUProfile()
}

func processData(file io.Reader, ctx models.AppContext) (map[osm.NodeID]*osm.Node, map[osm.WayID]*osm.Way, map[osm.RelationID]*osm.Relation, mapset.Set[osm.NodeID], *simple.WeightedDirectedGraph) {

	log.Info().Msg("started processing data")
	// UI
	infiniteLoadingBar := widget.NewProgressBarInfinite()
	infiniteLoadingBar.Start()
	loadingContainer := container.NewVBox(infiniteLoadingBar)
	ctx.Tabs.Items[1].Content = loadingContainer

	nodes := make(map[osm.NodeID]*osm.Node)
	ways := make(map[osm.WayID]*osm.Way)
	relations := make(map[osm.RelationID]*osm.Relation)
	footWays := mapset.NewSet[osm.NodeID]()

	g := simple.NewWeightedDirectedGraph(1, 0)

	// Create a PBF reader
	scanner := osmpbf.New(context.Background(), file, 4)

	// Scan and populate the relations map
	for scanner.Scan() {
		// Get the next OSM object
		obj := scanner.Object()

		switch v := obj.(type) {
		case *osm.Node:
			nodes[v.ID] = v
			g.AddNode(simple.Node(v.ID))
		case *osm.Way:
			ways[v.ID] = v
			// iterates through every node on every way
			nodeListLength := len(v.Nodes)
			for i, node := range v.Nodes {

				// creates an edge for every segment of the way
				/*
					If this is on the last node, there are no more segments to create
					(since the number of edges in a series of edges is node count - 1)
					and the edge creation must be skipped (otherwise array out of bounds)
				*/
				if i+1 != nodeListLength {
					if v.Tags.Find("highway") == "footway" || v.Tags.Find("highway") == "steps" {
						thisNode := nodes[v.Nodes[i].ID]
						nextNode := nodes[v.Nodes[i+1].ID]
						nodeDistance := geo.Distance(linebound.NodeToPoint(*thisNode), linebound.NodeToPoint(*nextNode))
						// disable routing through elevators
						if thisNode.Tags.Find("highway") == "elevator" || nextNode.Tags.Find("highway") == "elevator" {
						} else {

							// TODO: Add very high penalty for walking in the wrong direction of an escalator

							// only allow walking along escalators the right direction
							if v.Tags.Find("conveying") != "" && v.Tags.Find("conveying") == "forward" {
								g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(v.Nodes[i].ID), simple.Node(v.Nodes[i+1].ID), nodeDistance*0.5))
							} else if v.Tags.Find("conveying") != "" && v.Tags.Find("conveying") == "backward" {
								g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(v.Nodes[i+1].ID), simple.Node(v.Nodes[i].ID), nodeDistance*0.5))

							} else {
								// if it is only a basic walkway
								g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(v.Nodes[i].ID), simple.Node(v.Nodes[i+1].ID), nodeDistance))
								g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(v.Nodes[i+1].ID), simple.Node(v.Nodes[i].ID), nodeDistance))
							}
						}
					}
				}
				// checks if the way is a footpath
				if v.Tags.Find("highway") == "footway" {
					footWays.Add(node.ID)
				}
			}
		case *osm.Relation:
			relations[v.ID] = v
		default:
			// Handle other OSM object types if needed
		}
	}
	// Handle any errors that occurred during scanning
	if err := scanner.Err(); err != nil {
		log.Err(err).Msg("Error reading OSM PBF file: %v")
		dialog.ShowError(err, ctx.Window)
	}
	log.Info().Msg("done processing data")

	return nodes, ways, relations, footWays, g
}

func servicesAndPlatforms(
	file io.Reader,
	ctx models.AppContext,
	nodes map[osm.NodeID]*osm.Node,
	ways map[osm.WayID]*osm.Way,
	relations map[osm.RelationID]*osm.Relation,
	footWays mapset.Set[osm.NodeID],
	g *simple.WeightedDirectedGraph,
	searchTerm string,
) {
	infiniteProgress := widget.NewProgressBarInfinite()
	infiniteProgress.Start()
	ctx.Tabs.Items[1].Content = container.NewCenter(infiniteProgress)
	ctx.Tabs.DisableIndex(2)

	platformWays := make(map[osm.WayID]*osm.Way)
	platformRelations := make(map[osm.RelationID]*osm.Relation)
	relevantPlatformWays := mapset.NewSet[*osm.Way]()
	relevantPlatformRelations := mapset.NewSet[*osm.Relation]()

	platforms := make(map[osm.ElementID]models.PlatformItem)

	routes := make(map[osm.RelationID]*osm.Relation)
	var trainTracks []orb.Ring

	parseStart := time.Now()

	elapsed := time.Since(parseStart)
	log.Printf("Parsing took %s", elapsed)
	searchStart := time.Now()

	// Filter ways for platforms and paths
	for _, v := range ways {

		// platform proximity calculation
		railwayTag := v.Tags.Find("railway")
		validRailwayTags := map[string]bool{
			"rail":         true,
			"light_rail":   true,
			"tram":         true,
			"subway":       true,
			"narrow_gauge": true,
			"monorail":     true,
		}
		if validRailwayTags[railwayTag] {
			// for the first run, there's no previous node, therefore the firstRun variables is true
			var prevPoint orb.Point
			firstRun := true
			for _, node := range v.Nodes {
				if firstRun != true {
					var point orb.Point
					point[0] = nodes[node.ID].Lon
					point[1] = nodes[node.ID].Lat
					// the third argumenti is how big the bound is around the line
					localBound := orb.Ring(linebound.GetRotatedBoundWithPad(prevPoint, point, 3))
					trainTracks = append(trainTracks, localBound)
					prevPoint = point
				} else {
					var point orb.Point
					point[0] = nodes[node.ID].Lon
					point[1] = nodes[node.ID].Lat
					prevPoint = point
					firstRun = false
				}
			}
		}

		if (v.Tags.Find("railway") == "platform" || v.Tags.Find("public_transport") == "platform") && strings.Contains(v.Tags.Find("name"), searchTerm) {
			platformWays[v.ID] = v
			platforms[v.ElementID()] = models.PlatformItem{
				ElementID: v.ElementID(),
			}
		}
	}

	// Collect routes from relations
	for _, v := range relations {
		if v.Tags.Find("type") == "route" {
			routes[v.ID] = v
		}
		if (v.Tags.Find("railway") == "platform" || v.Tags.Find("public_transport") == "platform") && strings.Contains(v.Tags.Find("name"), searchTerm) {
			platformRelations[v.ID] = v
			platforms[v.ElementID()] = models.PlatformItem{
				ElementID: v.ElementID(),
			}
		}
	}
	elapsed = time.Since(searchStart)
	log.Printf("Search took %s", elapsed)

	// ==================================
	// Match stop positions with services
	// ==================================
	matchingStart := time.Now()

	// iterates through all routes in the entire city
	for _, route := range routes {

		// iterates through every node that is in a given route
		for _, routeMember := range route.Members {
			// iterates through every platform
			for platformID, platform := range platformWays {

				// checks if the ID of the platform matches
				if int64(platformID) == routeMember.Ref {
					relevantPlatformWays.Add(platform)

					elementID := platform.ElementID()
					currentPlatform := platforms[elementID]
					currentPlatform.Services = append(currentPlatform.Services, route)
					platforms[elementID] = currentPlatform

				}
			}
			for platformID, platform := range platformRelations {
				if routeMember.Type == "relation" {

					relationID, err := routeMember.ElementID().RelationID()
					if err != nil {
						log.Err(err).Msg("determining RelationID of platform " + fmt.Sprint(platformID) + " failed since it is not of type relation")
					}
					if platformID == relationID {
						relevantPlatformRelations.Add(platform)

						elementID := platform.ElementID()
						currentPlatform := platforms[elementID]
						currentPlatform.Services = append(currentPlatform.Services, route)
						platforms[elementID] = currentPlatform
					}
				}
			}
		}
	}
	elapsed = time.Since(matchingStart)
	log.Printf("Matching took %s", elapsed)

	platformData := color.New(color.Bold, color.FgWhite).PrintlnFunc()

	userPlatformList := models.PlatformList{
		Ways:      ways,
		Relations: relations,
	}

	for platformKey, genericPlatform := range platforms {
		platformType, err := platformKey.Type()
		if err != nil {
			log.Err(err).Msg("determining type of platform " + fmt.Sprint(genericPlatform.ElementID) + " failed")
		}
		switch platformType {
		case osm.TypeWay:
			wayID, err := platformKey.WayID()
			if err != nil {
				log.Err(err).Msg("determining WayID of platform " + fmt.Sprint(genericPlatform.ElementID) + " failed since it is not of type relation")
			}
			data := "Platform " + platformWays[wayID].Tags.Find("name") + " with ID " + fmt.Sprint(wayID) + " and type " + fmt.Sprint(platformType) + " has services:"
			platformData(strings.Repeat("=", len(data)))
			platformData(data)
			platformData(strings.Repeat("=", len(data)))
		case osm.TypeRelation:
			relationID, err := platformKey.RelationID()
			if err != nil {
				log.Err(err).Msg("determining RelationID of platform " + fmt.Sprint(genericPlatform.ElementID) + " failed since it is not of type relation")
			}
			data := "Platform " + platformRelations[relationID].Tags.Find("name") + " with ID " + fmt.Sprint(relationID) + " and type " + fmt.Sprint(platformType) + " has services:"
			platformData(strings.Repeat("=", len(data)))
			platformData(data)
			platformData(strings.Repeat("=", len(data)))
		}
		var serviceData []*osm.Relation

		for _, service := range genericPlatform.Services {
			printData := service.Tags.Find("name") + " with operator " + service.Tags.Find("operator") + " and vehicle type " + service.Tags.Find("route")

			red, green, blue, err := helpers.ColorFromString(service.Tags.Find("colour"))
			if err != nil {
				log.Warn().Msg("failed decoding color")
				fmt.Println(printData)
			} else {
				colorPrinter := color.RGB(255, 255, 255).AddBgRGB(int(red), int(green), int(blue))
				colorPrinter.Println(printData)
			}

			serviceData = append(serviceData, service)
		}
		currentUserPlatform := models.PlatformItem{
			ElementID: genericPlatform.ElementID,
			Services:  serviceData,
		}
		currentList := userPlatformList
		currentList.Platforms = append(currentList.Platforms, currentUserPlatform)
		userPlatformList = currentList
	}

	platformUIList := ui.NewPlatformSelector(userPlatformList)
	platformUIList.SourcePlatformChan = make(chan models.PlatformAndServiceSelection)
	platformUIList.DestPlatformChan = make(chan models.PlatformAndServiceSelection)

	ctx.Tabs.Items[1].Content = platformUIList

	sourcePlatformID := <-platformUIList.SourcePlatformChan
	destPlatformID := <-platformUIList.DestPlatformChan

	calcShortestPath(ctx, nodes, ways, relations, trainTracks, footWays, g, sourcePlatformID, destPlatformID)
}

type GeoJSON struct {
	Type       string      `json:"type"`
	Geometry   Geometry    `json:"geometry"`
	Properties interface{} `json:"properties"`
}

type Geometry struct {
	Type        string      `json:"type"`
	Coordinates [][]float64 `json:"coordinates"` // For LineString
}

func calcShortestPath(
	ctx models.AppContext,
	nodes map[osm.NodeID]*osm.Node,
	ways map[osm.WayID]*osm.Way,
	relations map[osm.RelationID]*osm.Relation,
	trainTracks []orb.Ring,
	footWays mapset.Set[osm.NodeID],
	g *simple.WeightedDirectedGraph,
	sourcePlatformAndService models.PlatformAndServiceSelection,
	destPlatformAndService models.PlatformAndServiceSelection,
) {

	relevantPlatformWays := mapset.NewSet[*osm.Way]()
	relevantPlatformRelations := mapset.NewSet[*osm.Relation]()

	infiniteLoadingBar := widget.NewProgressBarInfinite()
	infiniteLoadingBar.Start()
	loadingContainer := container.NewCenter(infiniteLoadingBar)
	ctx.Tabs.Items[2].Content = loadingContainer
	ctx.Tabs.EnableIndex(2)
	ctx.Tabs.SelectIndex(2)

	var sourceNodes []osm.NodeID
	var targetNodes []osm.NodeID

	platformSpines := make(map[osm.ElementID]models.PlatformSpine)

	sourcePlatformType, err := sourcePlatformAndService.Platform.Type()
	if sourcePlatformType == "way" {
		wayID, err := sourcePlatformAndService.Platform.WayID()
		if err != nil {
			log.Err(err).Msg("cannot get WayID of " + fmt.Sprint(sourcePlatformAndService.Platform))
		}
		relevantPlatformWays.Add(ways[wayID])
	} else if sourcePlatformType == "relation" {
		relationID, err := sourcePlatformAndService.Platform.RelationID()
		if err != nil {
			log.Err(err).Msg("cannot get RelationID of " + fmt.Sprint(sourcePlatformAndService.Platform))
		}
		relevantPlatformRelations.Add(relations[relationID])
	} else {
		log.Err(nil).Msg("source platform not of type way or relation")
	}

	destPlatformType, err := destPlatformAndService.Platform.Type()
	if destPlatformType == "way" {
		wayID, err := destPlatformAndService.Platform.WayID()
		if err != nil {
			log.Err(err).Msg("cannot get WayID of " + fmt.Sprint(destPlatformAndService.Platform))
		}
		relevantPlatformWays.Add(ways[wayID])
	} else if destPlatformType == "relation" {
		relationID, err := destPlatformAndService.Platform.RelationID()
		if err != nil {
			log.Err(err).Msg("cannot get RelationID of " + fmt.Sprint(destPlatformAndService.Platform))
		}
		relevantPlatformRelations.Add(relations[relationID])
	} else {
		log.Err(nil).Msg("dest platform not of type way or relation")
	}

	var allClosePoints []osm.Node

	closenessStart := time.Now()
	for platform := range relevantPlatformWays.Iterator().C {

		var platformNodes []osm.Node
		for _, wayNode := range platform.Nodes {
			platformNodes = append(platformNodes, *nodes[wayNode.ID])
		}

		currentSpine := platformSpines[platform.ElementID()]
		if platform.Tags.Find("area") == "yes" {
			linebound.SetPlatformSpine(ctx, platformNodes, platformSpines, trainTracks, nodes, platform.ElementID(), &allClosePoints)
		} else {
			startNode := linebound.NodeToPoint(*nodes[platform.Nodes[0].ID])
			endNode := linebound.NodeToPoint(*nodes[platform.Nodes[len(platform.Nodes)-1].ID])
			currentSpine.Start = startNode
			currentSpine.End = endNode
			log.Debug().Msg("Platform " + fmt.Sprint(platform.ElementID()) + " is not area and has spine " + fmt.Sprint(currentSpine))
		}
		for _, node := range platform.Nodes {
			if (nodes[node.ID].Tags.Find("level") != "") || footWays.Contains(node.ID) {
				if platform.ElementID() == sourcePlatformAndService.Platform {
					sourceNodes = append(sourceNodes, node.ID)
				}
				if platform.ElementID() == destPlatformAndService.Platform {
					targetNodes = append(targetNodes, node.ID)
				}
			}
		}
	}
	for platform := range relevantPlatformRelations.Iterator().C {
		// point nodes are all points on the platform. also includes inners, since that is an okay destination
		var platformPointNodes []osm.Node
		// spine search nodes should only be outers, since inners can confuse the algorithm since it should only be used the the outside of the platform
		var platformSpineSearchNodes []osm.Node

		var platformNumber string
		// sourcePlatformAndService models.PlatformAndServiceSelection,
		if platform.ElementID() == sourcePlatformAndService.Platform {
			platformNumber, err = getPlatformNumberOfService(sourcePlatformAndService.Platform, nodes, ways, relations, *relations[sourcePlatformAndService.Service])
			if err != nil {
				log.Warn().Msg("couldn't get the platform number of service relation/" + fmt.Sprint(sourcePlatformAndService.Service) + " at platform " + fmt.Sprint(sourcePlatformAndService.Platform))
			}
		}
		if platform.ElementID() == destPlatformAndService.Platform {
			platformNumber, err = getPlatformNumberOfService(destPlatformAndService.Platform, nodes, ways, relations, *relations[destPlatformAndService.Service])
			if err != nil {
				log.Warn().Msg("couldn't get the platform number of service relation/" + fmt.Sprint(destPlatformAndService.Service) + " at platform " + fmt.Sprint(destPlatformAndService.Platform))
			}
		}

		var platformEdges []*osm.Way
		foundPlatformEdge := false
		for _, member := range platform.Members {
			if member.Type == osm.TypeWay {
				wayID, err := member.ElementID().WayID()
				if err != nil {
					log.Err(err).Msg("determining WayID of platform member" + fmt.Sprint(member.ElementID()) + " failed since it is not of type way")
				}
				way := ways[wayID]
				// is platform edge and didn't find a matching platform edge already
				if way.Tags.Find("railway") == "platform_edge" && foundPlatformEdge == false {
					log.Debug().Msg("way " + fmt.Sprint(wayID) + " in relation " + fmt.Sprint(platform.ID) + " is platform_edge")
					// checks if any of the platform numbers are mentioned in a stop_position contained in the service relation with the same name
					if way.Tags.Find("ref") == platformNumber {
						foundPlatformEdge = true
						platformEdges = []*osm.Way{way}
					} else {
						platformEdges = append(platformEdges, way)
					}
				}
				for _, wayNode := range way.Nodes {
					// since way nodes don't have tags i need to find the original node in the map
					node := nodes[wayNode.ID]
					platformPointNodes = append(platformPointNodes, *node)
					if member.Role != "inner" {
						platformSpineSearchNodes = append(platformSpineSearchNodes, *node)
					}
				}
			}
		}

		if platformEdges == nil {
			linebound.SetPlatformSpine(ctx, platformSpineSearchNodes, platformSpines, trainTracks, nodes, platform.ElementID(), &allClosePoints)
		} else {
			var platformEdgeToUse osm.Way
			if len(platformEdges) == 1 {
				platformEdgeToUse = *platformEdges[0]
				log.Info().Msg("selected platform number " + fmt.Sprint(platformEdgeToUse.Tags.Find("ref")) + " for platform " + fmt.Sprint(platform.ElementID()))
			} else {
				platformEdgeToUseChan := make(chan osm.Way)
				ui.ShowPlatformEdgeSelector(ctx.Window, platformEdges, platformEdgeToUseChan)
				platformEdgeToUse = <-platformEdgeToUseChan
			}
			var edgeSpine models.PlatformSpine
			edgeSpine.Start = linebound.NodeToPoint(*nodes[platformEdgeToUse.Nodes[0].ID])
			edgeSpine.End = linebound.NodeToPoint(*nodes[platformEdgeToUse.Nodes[len(platformEdgeToUse.Nodes)-1].ID])
			log.Debug().Msg("edge spine: " + fmt.Sprint(edgeSpine))
			platformSpines[platform.ElementID()] = edgeSpine
		}
		for _, node := range platformPointNodes {
			if (nodes[node.ID].Tags.Find("level") != "") || footWays.Contains(node.ID) {
				if platform.ElementID() == sourcePlatformAndService.Platform {
					sourceNodes = append(sourceNodes, node.ID)
				}
				if platform.ElementID() == destPlatformAndService.Platform {
					targetNodes = append(targetNodes, node.ID)
				}
			}
		}
	}

	geoJsonCloseNodes := GeoJSON{
		Type: "Feature",
		Geometry: Geometry{
			Type: "LineString",
		},
	}

	// debug output
	for _, node := range allClosePoints {
		// Assuming the node ID corresponds to the OSM node ID
		if coord, exists := nodes[osm.NodeID(node.ID)]; exists {
			geoJsonCloseNodes.Geometry.Coordinates = append(geoJsonCloseNodes.Geometry.Coordinates, []float64{coord.Lon, coord.Lat})
		}
	}

	file, err := os.Create("close-nodes.geojson")
	if err != nil {
		log.Err(err).Msg("Error creating file:")
		dialog.ShowError(err, ctx.Window)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(geoJsonCloseNodes); err != nil {
		log.Err(err).Msg("Error encoding GeoJSON:")
		dialog.ShowError(err, ctx.Window)
	}

	elapsed := time.Since(closenessStart)
	log.Debug().Msg("Closeness checking took " + fmt.Sprint(elapsed) + "s")
	routingTime := time.Now()

	log.Info().Msg("source nodes: " + fmt.Sprint(sourceNodes))
	log.Info().Msg("target nodes: " + fmt.Sprint(targetNodes))

	var shortestPath []graph.Node
	var shortestWeight float64

	for _, sourceID := range sourceNodes {
		// Compute the shortest path tree from the source node
		shortest := path.DijkstraFrom(g.Node(int64(sourceID)), g)

		// Extract shortest paths to the destination nodes
		for _, destID := range targetNodes {
			if path, weight := shortest.To(int64(destID)); len(path) > 0 {
				shortestPath = path
				shortestWeight = weight
			}
		}
	}
	fmt.Printf("Shortest path: %v (weight: %v)\n", shortestPath, shortestWeight)

	var sourceExit osm.Node
	sourceExitFound := false
	var destExit osm.Node
	for _, graphNode := range shortestPath {
		node := nodes[osm.NodeID(graphNode.ID())]
		if node.Tags.Find("level") != "" {
			if sourceExitFound == false {
				sourceExitFound = true
				sourceExit = *node
			}
			destExit = *node
		}
	}
	elapsed = time.Since(routingTime)
	log.Printf("Routing took %s", elapsed)
	outputTime := time.Now()

	sourceSpine := platformSpines[sourcePlatformAndService.Platform]
	destSpine := platformSpines[destPlatformAndService.Platform]

	if sourceSpine == (models.PlatformSpine{}) {
		log.Debug().Msg(fmt.Sprint(platformSpines))
		error := errors.New("nil value in platform spine")
		log.Err(error).Msg("nil value in source platform spine")
		dialog.ShowError(error, ctx.Window)
	}
	if destSpine == (models.PlatformSpine{}) {
		log.Debug().Msg(fmt.Sprint(platformSpines))
		error := errors.New("nil value in platform spine")
		log.Err(error).Msg("nil value in dest platform spine")
		dialog.ShowError(error, ctx.Window)
	}
	log.Debug().Msg("source spine: " + fmt.Sprint(sourceSpine))
	log.Debug().Msg("dest spine: " + fmt.Sprint(destSpine))

	log.Info().Msg("correcting source spine orientations")
	sourceSpine = correctSpineOrientation(sourceSpine, sourcePlatformAndService, nodes, relations)
	log.Info().Msg("correcting dest spine orientations")
	destSpine = correctSpineOrientation(destSpine, destPlatformAndService, nodes, relations)

	log.Debug().Msg("source spine modified: " + fmt.Sprint(sourceSpine))
	log.Debug().Msg("dest spine modified: " + fmt.Sprint(destSpine))

	sourcePoint0 := linebound.OrbPointToGeoPoint(sourceSpine.Start)
	sourcePoint1 := linebound.OrbPointToGeoPoint(sourceSpine.End)

	destPoint0 := linebound.OrbPointToGeoPoint(destSpine.Start)
	destPoint1 := linebound.OrbPointToGeoPoint(destSpine.End)

	sourceExitPoint := linebound.OrbPointToGeoPoint(linebound.NodeToPoint(sourceExit))
	destExitPoint := linebound.OrbPointToGeoPoint(linebound.NodeToPoint(destExit))

	sourcePlatformEdgePoint := s2.Project(sourceExitPoint, sourcePoint0, sourcePoint1)
	log.Debug().Msg(fmt.Sprint(linebound.GeoPointToOrbPoint(destPoint0)))
	destPlatformEdgePoint := s2.Project(destExitPoint, destPoint0, destPoint1)

	sourceOptimalDoor := linebound.GeoPointToOrbPoint(sourcePlatformEdgePoint)
	destOptimalDoor := linebound.GeoPointToOrbPoint(destPlatformEdgePoint)
	log.Info().Msg("optimal spots:")
	log.Info().Msg(fmt.Sprint(sourceOptimalDoor))
	log.Info().Msg(fmt.Sprint(destOptimalDoor))

	sourcePlatformLength := geo.DistanceHaversine(sourceSpine.Start, sourceSpine.End)
	fromPlatformStart := geo.DistanceHaversine(sourceSpine.Start, sourceOptimalDoor)
	destPlatformLength := geo.DistanceHaversine(destSpine.Start, destSpine.End)
	toPlatformStart := geo.DistanceHaversine(destSpine.Start, destOptimalDoor)

	alongSourcePlatform := fromPlatformStart / sourcePlatformLength
	alongDestPlatform := toPlatformStart / destPlatformLength

	log.Info().Msg("along source platform: " + fmt.Sprint(alongSourcePlatform*100) + "% or " + fmt.Sprint(fromPlatformStart) + "m")
	log.Info().Msg("along dest platform: " + fmt.Sprint(alongDestPlatform*100) + "% or " + fmt.Sprint(toPlatformStart))

	log.Info().Msg("starting exit: " + fmt.Sprint(sourceExit.ID))
	log.Info().Msg("ending exit: " + fmt.Sprint(destExit.ID))

	geo := GeoJSON{
		Type: "Feature",
		Geometry: Geometry{
			Type: "LineString",
		},
	}

	ui.DisplayResults(ctx, alongSourcePlatform, alongDestPlatform, fromPlatformStart, toPlatformStart)

	for _, node := range shortestPath {
		// Assuming the node ID corresponds to the OSM node ID
		if coord, exists := nodes[osm.NodeID(node.ID())]; exists {
			geo.Geometry.Coordinates = append(geo.Geometry.Coordinates, []float64{coord.Lon, coord.Lat})
		}
	}

	file, err = os.Create("path.geojson")
	if err != nil {
		log.Err(err).Msg("Error creating file:")
		dialog.ShowError(err, ctx.Window)
	}
	defer file.Close()

	encoder = json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(geo); err != nil {
		log.Err(err).Msg("Error encoding GeoJSON:")
		dialog.ShowError(err, ctx.Window)
	}
	elapsed = time.Since(outputTime)
	log.Printf("Routing and output took %s", elapsed)
}

func correctSpineOrientation(inputSpine models.PlatformSpine, selection models.PlatformAndServiceSelection, nodes map[osm.NodeID]*osm.Node, relations map[osm.RelationID]*osm.Relation) models.PlatformSpine {
	serviceObject := relations[selection.Service]

	// fmt.Println(serviceObject)
	fmt.Println(selection.Platform.FeatureID())

	foundSelectedPlatform := false

	var nextStop osm.Member

	for _, member := range serviceObject.Members {
		if member.ElementID().FeatureID() == selection.Platform.FeatureID() {
			foundSelectedPlatform = true
		}

		if foundSelectedPlatform == true && member.Type == "node" {
			nextStop = member
			break
		}
	}

	log.Debug().Msg("selected platform " + fmt.Sprint(selection.Platform.FeatureID()) + " has next stop " + fmt.Sprint(nextStop.FeatureID()))

	var nextStopPoint orb.Point
	nextStopType, err := nextStop.ElementID().Type()
	if err != nil {
		log.Err(err).Msg("unknown type for element " + fmt.Sprint(nextStop))
	}
	if nextStopType == "node" {
		nextStopNodeID, err := nextStop.ElementID().NodeID()
		if err != nil {
			log.Err(err).Msg("can't get NodeID of next stop since it is not of type node")
		}
		node := nodes[nextStopNodeID]
		nextStopPoint = linebound.NodeToPoint(*node)
	}

	log.Debug().Msg("spine: " + fmt.Sprint(inputSpine))

	log.Debug().Msg("stop point: " + fmt.Sprint(nextStopPoint))

	spineStartNodeDistance := geo.Distance(inputSpine.Start, nextStopPoint)

	spineEndNodeDistance := geo.Distance(inputSpine.End, nextStopPoint)

	log.Debug().Msg("spine start node distance to next stop: " + fmt.Sprint(spineStartNodeDistance))
	log.Debug().Msg("spine end node distance to next stop: " + fmt.Sprint(spineEndNodeDistance))

	temp := inputSpine
	if spineEndNodeDistance < spineStartNodeDistance {
		inputSpine.Start = temp.End
		inputSpine.End = temp.Start
		log.Debug().Msg("switching around")
	} else {
		log.Debug().Msg("not switching around")
	}

	log.Debug().Msg("updated platform spine: " + fmt.Sprint(inputSpine))

	return inputSpine
}

func getPlatformNumberOfService(platformID osm.ElementID, nodes map[osm.NodeID]*osm.Node, ways map[osm.WayID]*osm.Way, relations map[osm.RelationID]*osm.Relation, service osm.Relation) (string, error) {
	platformType, err := platformID.Type()
	if err != nil {
		log.Err(err).Msg("platform " + fmt.Sprint(platformID) + " has unknown type")
	}

	var platformName string

	switch platformType {
	case "way":
		wayID, err := platformID.WayID()
		if err != nil {
			log.Err(err).Msg("platform " + fmt.Sprint(platformID) + " is of type way but does not have a WayID")
		}
		platform := ways[wayID]
		platformName = platform.Tags.Find("name")
	case "relation":
		relationID, err := platformID.RelationID()
		if err != nil {
			log.Err(err).Msg("platform " + fmt.Sprint(platformID) + " is of type relation but does not have a RelationID")
		}
		platform := relations[relationID]
		platformName = platform.Tags.Find("name")
	default:
		errorMessage := "platform " + fmt.Sprint(platformID) + " is neither way or relation"
		log.Err(nil).Msg(errorMessage)
		return "", errors.New(errorMessage)
	}
	if platformName == "" {
		errorMessage := "platform " + fmt.Sprint(platformID) + " has no name"
		err := errors.New(errorMessage)
		log.Err(err).Msg(errorMessage)
		return "", err
	}

	var stopPosition *osm.Node
	log.Debug().Msg("searching for stop position with name " + platformName)
	for _, member := range service.Members {
		if member.Type == "node" {
			nodeID, err := member.ElementID().NodeID()
			if err != nil {
				log.Err(err).Msg("member " + fmt.Sprint(member.ElementID()) + " is of type node but does not have a node id")
			}
			stopPosition = nodes[nodeID]
			if stopPosition == nil {
				log.Debug().Msg("node " + fmt.Sprint(nodeID) + " cannot be found in nodes map")
			} else {
				stopPositionName := stopPosition.Tags.Find("name")
				log.Debug().Msg("stop position id: " + fmt.Sprint(stopPosition.ElementID()) + " with name " + fmt.Sprint(stopPositionName))
				if stopPositionName == platformName {
					break
				}
			}
		}
	}

	var platformNumberString string

	ref := stopPosition.Tags.Find("ref")
	localRef := stopPosition.Tags.Find("local_ref")

	// the local_ref tag is preferred to the ref tag since the ref tag sometimes containers global identification and only the local_ref tag would provide just the local platform numbers
	if ref == "" && localRef == "" {
		errMessage := "no platform numbers found in stop position " + fmt.Sprint(stopPosition.ElementID()) + " for platform " + fmt.Sprint(platformID)
		log.Err(err).Msg(errMessage)
		return "", errors.New(errMessage)
	} else if localRef != "" {
		platformNumberString = localRef
	} else if localRef == "" && ref != "" {
		platformNumberString = ref
	} else {
		log.Err(nil).Msg("this shouldn't be reachable")
	}

	return platformNumberString, nil
}
