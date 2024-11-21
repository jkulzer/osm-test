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

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geo"
	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"

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

	doneProcessing := make(chan bool)

	// UI setup
	loadButton := container.NewVBox(progressBarOsmParsing)
	// Layout the UI components
	mainMenu := container.NewVBox(
		widget.NewLabel("Platform Routing Application"),
		searchTermEntry,
		loadButton,
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

	var reader fyne.URIReadCloser
	readerChan := make(chan fyne.URIReadCloser)
	errChan := make(chan error)

	go func() {
		// uri, err := storage.ParseURI("file://berlin-latest.osm.pbf")
		// if err != nil {
		// 	log.Error().Err(err).Msg("Failed to parse uri")
		// 	dialog.ShowError(err, w)
		// }

		ui.ShowFilePicker(w, readerChan, errChan)

		reader := <-readerChan
		err := <-errChan
		// reader, err = storage.Reader(uri)
		if err != nil {
			log.Error().Err(err).Msg("Failed to open OSM file")
			dialog.ShowError(err, w)
		}
		nodes, ways, relations, footWays, graph = processData(reader, ctx)
		doneProcessing <- true
	}()

	go func() {
		// _ = <-readerChan
		for i := 0.0; i <= 0.95; i += 0.01 {
			time.Sleep(time.Millisecond * 60)
			progressBarOsmParsing.SetValue(i)
		}

		// after data parsing is done
		_ = <-doneProcessing
		progressBarOsmParsing.SetValue(0.99)
		button := container.NewVBox(widget.NewButton("Load Data", func() {
			log.Info().Msg("started processing input data")
			ctx.Tabs.EnableIndex(1)
			ctx.Tabs.SelectIndex(1)
			// Call data parsing function
			go func() {
				servicesAndPlatforms(reader, ctx, nodes, ways, relations, footWays, graph, searchTermEntry.Text)
			}()
		}))
		viewport1 := container.NewVBox(
			widget.NewLabel("Platform Routing Application"),
			searchTermEntry,
			button,
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

func servicesAndPlatforms(file io.Reader, ctx models.AppContext, nodes map[osm.NodeID]*osm.Node, ways map[osm.WayID]*osm.Way, relations map[osm.RelationID]*osm.Relation, footWays mapset.Set[osm.NodeID], g *simple.WeightedDirectedGraph, searchTerm string) {
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

					if platformID == routeMember.ElementID().RelationID() {
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
		switch platformKey.Type() {
		case osm.TypeWay:
			data := "Platform " + platformWays[platformKey.WayID()].Tags.Find("name") + " with ID " + fmt.Sprint(platformKey.WayID()) + " and type " + fmt.Sprint(platformKey.Type()) + " has services:"
			platformData(strings.Repeat("=", len(data)))
			platformData(data)
			platformData(strings.Repeat("=", len(data)))
		case osm.TypeRelation:
			data := "Platform " + platformRelations[platformKey.RelationID()].Tags.Find("name") + " with ID " + fmt.Sprint(platformKey.WayID()) + " and type " + fmt.Sprint(platformKey.Type()) + " has services:"
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
	platformUIList.SourcePlatformChan = make(chan osm.ElementID)
	platformUIList.DestPlatformChan = make(chan osm.ElementID)

	ctx.Tabs.Items[1].Content = platformUIList

	sourcePlatformID := <-platformUIList.SourcePlatformChan
	destPlatformID := <-platformUIList.DestPlatformChan

	calcShortestPath(ctx, nodes, ways, relations, relevantPlatformWays, relevantPlatformRelations, trainTracks, footWays, g, sourcePlatformID, destPlatformID)
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

func calcShortestPath(ctx models.AppContext, nodes map[osm.NodeID]*osm.Node, ways map[osm.WayID]*osm.Way, relations map[osm.RelationID]*osm.Relation, relevantPlatformWays mapset.Set[*osm.Way], relevantPlatformRelations mapset.Set[*osm.Relation], trainTracks []orb.Ring, footWays mapset.Set[osm.NodeID], g *simple.WeightedDirectedGraph, sourcePlatform osm.ElementID, destPlatform osm.ElementID) {

	infiniteLoadingBar := widget.NewProgressBarInfinite()
	infiniteLoadingBar.Start()
	loadingContainer := container.NewCenter(infiniteLoadingBar)
	ctx.Tabs.Items[2].Content = loadingContainer
	ctx.Tabs.EnableIndex(2)
	ctx.Tabs.SelectIndex(2)

	var sourceNodes []osm.NodeID
	var targetNodes []osm.NodeID

	platformSpines := make(map[osm.ElementID][2]orb.Point)

	closenessStart := time.Now()
	for platform := range relevantPlatformWays.Iterator().C {

		var platformNodes []osm.Node
		for _, wayNode := range platform.Nodes {
			platformNodes = append(platformNodes, *nodes[wayNode.ID])
		}

		currentSpine := platformSpines[platform.ElementID()]
		if platform.Tags.Find("area") == "yes" {
			linebound.SetPlatformSpine(ctx, platformNodes, platformSpines, trainTracks, nodes, platform.ElementID())
		} else {
			startNode := linebound.NodeToPoint(*nodes[platform.Nodes[0].ID])
			endNode := linebound.NodeToPoint(*nodes[platform.Nodes[len(platform.Nodes)-1].ID])
			currentSpine[0] = startNode
			currentSpine[1] = endNode
		}
		for _, node := range platform.Nodes {
			if (nodes[node.ID].Tags.Find("level") != "") || footWays.Contains(node.ID) {
				if platform.ElementID() == sourcePlatform {
					sourceNodes = append(sourceNodes, node.ID)
				}
				if platform.ElementID() == destPlatform {
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

		var platformEdges []*osm.Way
		for _, member := range platform.Members {
			// log.Debug().Msg("member has type " + fmt.Sprint(member.Type))
			if member.Type == osm.TypeWay {
				// if member.Role != "inner" {
				wayID := member.ElementID().WayID()
				way := ways[wayID]
				if way.Tags.Find("railway") == "platform_edge" {
					log.Debug().Msg("way " + fmt.Sprint(wayID) + " in relation " + fmt.Sprint(platform.ID) + " is platform_edge")
					platformEdges = append(platformEdges, way)
				}
				for _, wayNode := range way.Nodes {
					// since way nodes don't have tags i need to find the original node in the map
					node := nodes[wayNode.ID]
					platformPointNodes = append(platformPointNodes, *node)
					if member.Role != "inner" {
						platformSpineSearchNodes = append(platformSpineSearchNodes, *node)
					}
				}
				// }
			}
		}

		if platformEdges == nil {
			linebound.SetPlatformSpine(ctx, platformSpineSearchNodes, platformSpines, trainTracks, nodes, platform.ElementID())
		} else {
			log.Debug().Msg(fmt.Sprint(platformEdges))
			platformEdgeToUse := platformEdges[0]
			var edgeSpine [2]orb.Point
			edgeSpine[0] = linebound.NodeToPoint(*nodes[platformEdgeToUse.Nodes[0].ID])
			edgeSpine[1] = linebound.NodeToPoint(*nodes[platformEdgeToUse.Nodes[len(platformEdgeToUse.Nodes)-1].ID])
			log.Debug().Msg("edge spine: " + fmt.Sprint(edgeSpine))
			platformSpines[platform.ElementID()] = edgeSpine
		}
		for _, node := range platformPointNodes {
			if (nodes[node.ID].Tags.Find("level") != "") || footWays.Contains(node.ID) {
				if platform.ElementID() == sourcePlatform {
					sourceNodes = append(sourceNodes, node.ID)
				}
				if platform.ElementID() == destPlatform {
					targetNodes = append(targetNodes, node.ID)
				}
			}
		}
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

	sourceSpine := platformSpines[sourcePlatform]
	destSpine := platformSpines[destPlatform]

	if sourceSpine == [2]orb.Point{} {
		log.Debug().Msg(fmt.Sprint(platformSpines))
		error := errors.New("nil value in platform spine")
		log.Err(error).Msg("nil value in source platform spine")
		dialog.ShowError(error, ctx.Window)
	}
	if destSpine == [2]orb.Point{} {
		log.Debug().Msg(fmt.Sprint(platformSpines))
		error := errors.New("nil value in platform spine")
		log.Err(error).Msg("nil value in dest platform spine")
		dialog.ShowError(error, ctx.Window)
	}
	log.Debug().Msg("source spine: " + fmt.Sprint(sourceSpine))
	log.Debug().Msg("dest spine: " + fmt.Sprint(destSpine))

	sourcePoint0 := linebound.OrbPointToGeoPoint(sourceSpine[0])
	sourcePoint1 := linebound.OrbPointToGeoPoint(sourceSpine[1])

	destPoint0 := linebound.OrbPointToGeoPoint(destSpine[0])
	destPoint1 := linebound.OrbPointToGeoPoint(destSpine[1])

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

	sourcePlatformLength := geo.DistanceHaversine(sourceSpine[0], sourceSpine[1])
	fromPlatformStart := geo.DistanceHaversine(sourceSpine[0], sourceOptimalDoor)
	destPlatformLength := geo.DistanceHaversine(destSpine[0], destSpine[1])
	toPlatformStart := geo.DistanceHaversine(destSpine[0], destOptimalDoor)

	alongSourcePlatform := fromPlatformStart / sourcePlatformLength
	alongDestPlatform := toPlatformStart / destPlatformLength

	log.Info().Msg("along source platform: " + fmt.Sprint(alongSourcePlatform*100) + "%")
	log.Info().Msg("along dest platform: " + fmt.Sprint(alongDestPlatform*100) + "%")

	log.Info().Msg("starting exit: " + fmt.Sprint(sourceExit.ID))
	log.Info().Msg("ending exit: " + fmt.Sprint(destExit.ID))

	geo := GeoJSON{
		Type: "Feature",
		Geometry: Geometry{
			Type: "LineString",
		},
	}

	ui.DisplayResults(ctx, alongSourcePlatform, alongDestPlatform)

	for _, node := range shortestPath {
		// Assuming the node ID corresponds to the OSM node ID
		if coord, exists := nodes[osm.NodeID(node.ID())]; exists {
			geo.Geometry.Coordinates = append(geo.Geometry.Coordinates, []float64{coord.Lon, coord.Lat})
		}
	}

	file, err := os.Create("path.geojson")
	if err != nil {
		log.Err(err).Msg("Error creating file:")
		dialog.ShowError(err, ctx.Window)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(geo); err != nil {
		log.Err(err).Msg("Error encoding GeoJSON:")
		dialog.ShowError(err, ctx.Window)
	}
	elapsed = time.Since(outputTime)
	log.Printf("Routing and output took %s", elapsed)
}
