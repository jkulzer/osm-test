package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/golang/geo/s2"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/jkulzer/osm-test/linebound"
	"github.com/jkulzer/osm-test/models"

	// logging
	// "github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	// "github.com/go-chi/chi/v5"
	// "github.com/go-chi/chi/v5/middleware"

	mapset "github.com/deckarep/golang-set/v2"

	"github.com/fatih/color"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geo"
	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"

	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/path"
	"gonum.org/v1/gonum/graph/simple"
)

type GeoJSON struct {
	Type       string      `json:"type"`
	Geometry   Geometry    `json:"geometry"`
	Properties interface{} `json:"properties"`
}

type Geometry struct {
	Type        string      `json:"type"`
	Coordinates [][]float64 `json:"coordinates"` // For LineString
}

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

	a := app.New()
	w := a.NewWindow("Platform Routing App")
	ctx.Window = w

	searchTermEntry := widget.NewEntry()
	searchTermEntry.SetPlaceHolder("Enter station name, e.g., 'Warschauer Straße'")

	statusLabel := widget.NewLabel("Status: Ready")
	file, err := os.Open("berlin-latest.osm.pbf")
	if err != nil {
		statusLabel.SetText("Failed to open OSM file")
		log.Error().Err(err).Msg("Failed to open OSM file")
		return
	}

	nodes, ways, relations, footWays, graph := processData(file, ctx)

	loadButton := widget.NewButton("Load Data", func() {

		// Call your data loading and parsing functions here
		go func() {
			resultCalculation(file, ctx, nodes, ways, relations, footWays, graph)
			defer file.Close()
			statusLabel.SetText("Data loaded successfully")
		}()
	})

	searchButton := widget.NewButton("Search", func() {
		searchTerm := searchTermEntry.Text
		if searchTerm == "" {
			statusLabel.SetText("Please enter a search term")
			return
		}

		// Run the platform search function based on the search term
		go func() {
			statusLabel.SetText(fmt.Sprintf("Searching for '%s'...", searchTerm))
			// Here, you would call your function to search platforms, e.g., getPlatforms(file, searchTerm)
			// For simplicity, I just simulate it here
			statusLabel.SetText(fmt.Sprintf("Found platforms for '%s'", searchTerm))
		}()
	})

	// Layout the UI components
	content := container.NewVBox(
		widget.NewLabel("Platform Routing Application"),
		searchTermEntry,
		loadButton,
		searchButton,
		statusLabel,
	)

	w.SetContent(content)
	w.ShowAndRun()

	pprof.StopCPUProfile()
}

func processData(file *os.File, ctx models.AppContext) (map[osm.NodeID]*osm.Node, map[osm.WayID]*osm.Way, map[osm.RelationID]*osm.Relation, mapset.Set[osm.NodeID], *simple.WeightedDirectedGraph) {

	log.Info().Msg("started processing data")
	// UI
	infiniteLoadingBar := widget.NewProgressBarInfinite()
	infiniteLoadingBar.Start()
	loadingContainer := container.NewVBox(infiniteLoadingBar)
	ctx.Window.SetContent(loadingContainer)

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
	}
	log.Info().Msg("done processing data")

	return nodes, ways, relations, footWays, g
}

func resultCalculation(file *os.File, ctx models.AppContext, nodes map[osm.NodeID]*osm.Node, ways map[osm.WayID]*osm.Way, relations map[osm.RelationID]*osm.Relation, footWays mapset.Set[osm.NodeID], g *simple.WeightedDirectedGraph) {

	platformWays := make(map[osm.WayID]*osm.Way)
	platformRelations := make(map[osm.RelationID]*osm.Relation)
	relevantPlatformWays := mapset.NewSet[*osm.Way]()
	relevantPlatformRelations := mapset.NewSet[*osm.Relation]()
	start := time.Now()

	platforms := make(map[PlatformKey]GenericPlatform)

	routes := make(map[osm.RelationID]*osm.Relation)
	var trainTracks []orb.Ring

	parseStart := time.Now()

	searchTerm := "Warschauer Straße"

	elapsed := time.Since(parseStart)
	log.Printf("Parsing took %s", elapsed)
	searchStart := time.Now()

	ctx.Window.SetContent(widget.NewLabel("data processing done"))
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

			// adds to the generic platform list
			platformKey := PlatformKey{Type: osm.TypeWay, ID: int64(v.ID)}
			platforms[platformKey] = GenericPlatform{
				Services: mapset.NewSet[*osm.Relation](),
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

			// adds to the generic platform list
			platformKey := PlatformKey{Type: osm.TypeRelation, ID: int64(v.ID)}
			platforms[platformKey] = GenericPlatform{
				Services: mapset.NewSet[*osm.Relation](),
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
			// for stopID, stopPosition := range stopPositions {
			// 	if routeMember.Type == osm.TypeNode && stopID == routeMember.Ref {
			// 		fmt.Printf(
			// 			"Stop: %s with service %s with ID %d\n",
			// 			stopPosition.Tags.Find("name"),
			// 			route.Tags.Find("name"),
			// 			stopPosition.ID,
			// 		)
			// 	}
			// }

			// iterates through every platform
			for platformID, platform := range platformWays {

				// checks if the ID of the platform matches
				if int64(platformID) == routeMember.Ref {
					relevantPlatformWays.Add(platform)

					platformKey := PlatformKey{Type: osm.TypeWay, ID: int64(platform.ID)}
					platforms[platformKey].Services.Add(route)

				}
			}
			for platformID, platform := range platformRelations {
				if routeMember.Type == "relation" {

					if platformID == routeMember.ElementID().RelationID() {
						relevantPlatformRelations.Add(platform)

						platformKey := PlatformKey{Type: osm.TypeRelation, ID: int64(platform.ID)}
						platforms[platformKey].Services.Add(route)
					}
				}
			}
		}
	}
	elapsed = time.Since(matchingStart)
	log.Printf("Matching took %s", elapsed)

	platformData := color.New(color.Bold, color.FgWhite).PrintlnFunc()

	for platformKey, genericPlatform := range platforms {
		switch platformKey.Type {
		case osm.TypeWay:
			data := "Platform " + platformWays[osm.WayID(platformKey.ID)].Tags.Find("name") + " with ID " + fmt.Sprint(platformKey.ID) + " and type " + fmt.Sprint(platformKey.Type) + " has services:"
			platformData(strings.Repeat("=", len(data)))
			platformData(data)
			platformData(strings.Repeat("=", len(data)))
		case osm.TypeRelation:
			data := "Platform " + platformRelations[osm.RelationID(platformKey.ID)].Tags.Find("name") + " with ID " + fmt.Sprint(platformKey.ID) + " and type " + fmt.Sprint(platformKey.Type) + " has services:"
			platformData(strings.Repeat("=", len(data)))
			platformData(data)
			platformData(strings.Repeat("=", len(data)))
		}
		for service := range genericPlatform.Services.Iterator().C {
			printData := service.Tags.Find("name") + " with operator " + service.Tags.Find("operator") + " and vehicle type " + service.Tags.Find("route")
			if len(service.Tags.Find("colour")) == 7 {

				red, err := strconv.ParseInt(service.Tags.Find("colour")[1:3], 16, 16)
				if err != nil {
					log.Warn().Msg("failed decoding line color")
				}
				green, err := strconv.ParseInt(service.Tags.Find("colour")[3:5], 16, 16)
				if err != nil {
					log.Warn().Msg("failed decoding line color")
				}
				blue, err := strconv.ParseInt(service.Tags.Find("colour")[5:7], 16, 16)
				if err != nil {
					log.Warn().Msg("failed decoding line color")
				}
				color.RGB(255, 255, 255).AddBgRGB(int(red), int(green), int(blue)).Println(printData)
			} else {
				fmt.Println(printData)
			}
		}
	}

	ctx.Window.SetContent(widget.NewLabel(fmt.Sprint(platformWays) + "\n" + fmt.Sprint(platformRelations)))

	// Brandenburger Tor Test
	// sourcePlatform := int64(237221908)
	// destPlatform := int64(11762778)

	// Warschauer Strasse Test
	sourcePlatform := ways[52580085].ElementID()
	destPlatform := relations[11765290].ElementID()

	// Prinzenstraße Test
	// sourcePlatform := ways[49038087].ElementID()
	// destPlatform := ways[49038086].ElementID()

	// Alexanderplatz Test
	// sourcePlatform := int64(3637944)
	// destPlatform := int64(3637412)

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
		var platformNodes []osm.Node
		var platformEdges []*osm.Way
		for _, member := range platform.Members {
			log.Debug().Msg("member has type " + fmt.Sprint(member.Type))
			if member.Type == osm.TypeWay {
				wayID := member.ElementID().WayID()
				way := ways[wayID]
				if way.Tags.Find("railway") == "platform_edge" {
					log.Debug().Msg("way " + fmt.Sprint(wayID) + " in relation " + fmt.Sprint(platform.ID) + " is platform_edge")
					platformEdges = append(platformEdges, way)
				}
				for _, wayNode := range way.Nodes {
					// since way nodes don't have tags i need to find the original node in the map
					node := nodes[wayNode.ID]
					platformNodes = append(platformNodes, *node)
				}
			}
		}

		if platformEdges == nil {
			linebound.SetPlatformSpine(ctx, platformNodes, platformSpines, trainTracks, nodes, platform.ElementID())
		} else {
			log.Debug().Msg(fmt.Sprint(platformEdges))
			platformEdgeToUse := platformEdges[0]
			var edgeSpine [2]orb.Point
			edgeSpine[0] = linebound.NodeToPoint(*nodes[platformEdgeToUse.Nodes[0].ID])
			edgeSpine[1] = linebound.NodeToPoint(*nodes[platformEdgeToUse.Nodes[len(platformEdgeToUse.Nodes)-1].ID])
			log.Debug().Msg("edge spine: " + fmt.Sprint(edgeSpine))
			platformSpines[platform.ElementID()] = edgeSpine
		}
		for _, node := range platformNodes {
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
	elapsed = time.Since(closenessStart)
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
		log.Err(errors.New("nil value in platform spine")).Msg("nil value in source platform spine")
	}
	if destSpine == [2]orb.Point{} {
		log.Debug().Msg(fmt.Sprint(platformSpines))
		log.Err(errors.New("nil value in platform spine")).Msg("nil value in dest platform spine")
	}

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

	for _, node := range shortestPath {
		// Assuming the node ID corresponds to the OSM node ID
		if coord, exists := nodes[osm.NodeID(node.ID())]; exists {
			geo.Geometry.Coordinates = append(geo.Geometry.Coordinates, []float64{coord.Lon, coord.Lat})
		}
	}

	file, err := os.Create("path.geojson")
	if err != nil {
		log.Err(err).Msg("Error creating file:")
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(geo); err != nil {
		log.Err(err).Msg("Error encoding GeoJSON:")
		return
	}
	elapsed = time.Since(outputTime)
	log.Printf("Routing and output took %s", elapsed)

	elapsed = time.Since(start)
	log.Printf("Done in %s", elapsed)
}
