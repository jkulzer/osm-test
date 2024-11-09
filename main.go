package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/golang/geo/s2"
	"log"
	"math"
	"net/http"
	_ "net/http/pprof"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jkulzer/osm-test/linebound"

	// "github.com/go-chi/chi/v5"
	// "github.com/go-chi/chi/v5/middleware"

	mapset "github.com/deckarep/golang-set/v2"

	"github.com/fatih/color"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geo"
	orbGeojson "github.com/paulmach/orb/geojson"
	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"

	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/path"
	"gonum.org/v1/gonum/graph/simple"
	// "fyne.io/fyne/v2/app"
	// "fyne.io/fyne/v2/widget"
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

func main() {
	fmt.Println("Data from:")
	fmt.Println("© OpenStreetMap contributors: https://openstreetmap.org/copyright")
	go func() {
		http.ListenAndServe("localhost:6060", nil)
	}()

	// a := app.New()
	// w := a.NewWindow("Hello World")
	//
	// w.SetContent(widget.NewLabel("Hello World!"))
	// w.ShowAndRun()

	// Open the OSM PBF file
	file, err := os.Open("berlin-latest.osm.pbf")
	if err != nil {
		log.Fatal("Error opening the file:", err)
	}
	defer file.Close()

	// Get platforms and print related train services
	getPlatforms(file)

}

func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	// Convert degrees to radians
	lat1Rad := lat1 * math.Pi / 180
	lon1Rad := lon1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	lon2Rad := lon2 * math.Pi / 180

	// Haversine formula
	dLat := lat2Rad - lat1Rad
	dLon := lon2Rad - lon1Rad

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	// Distance in kilometers
	return orb.EarthRadius * c
}

func getPlatforms(file *os.File) {

	// maxDistance := 0.000000001
	// maxDistance := 0.0
	searchTerm := "Prinzenstraße"

	// Maps for storing OSM data
	nodes := make(map[int64]*osm.Node)
	ways := make(map[int64]*osm.Way)
	relations := make(map[int64]*osm.Relation)
	platformWays := make(map[int64]*osm.Way)
	platformRelations := make(map[int64]*osm.Relation)
	relevantPlatformWays := mapset.NewSet[*osm.Way]()
	relevantPlatformRelations := mapset.NewSet[*osm.Relation]()
	footWays := mapset.NewSet[osm.NodeID]()

	platforms := make(map[PlatformKey]GenericPlatform)

	stopPositions := make(map[int64]*osm.Node)
	routes := make(map[int64]*osm.Relation)

	// train tracks (platform edge detection)
	var trainTracks []orb.Ring

	// Create a PBF reader
	scanner := osmpbf.New(context.Background(), file, 4)
	g := simple.NewWeightedDirectedGraph(1, 0)
	start := time.Now()

	// Scan and populate the relations map
	for scanner.Scan() {
		// Get the next OSM object
		obj := scanner.Object()

		switch v := obj.(type) {
		case *osm.Node:
			nodes[int64(v.ID)] = v
			g.AddNode(simple.Node(v.ID))
		case *osm.Way:
			ways[int64(v.ID)] = v
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
						thisNode := nodes[int64(v.Nodes[i].ID)]
						nextNode := nodes[int64(v.Nodes[i+1].ID)]
						nodeDistance := Haversine(thisNode.Lat, thisNode.Lon, nextNode.Lat, nextNode.Lon)
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
			relations[int64(v.ID)] = v
		default:
			// Handle other OSM object types if needed
		}
	}
	elapsed := time.Since(start)
	log.Printf("Parsing took %s", elapsed)

	// Filter nodes for stop positions
	for _, v := range nodes {
		// if isPartOfRoute(v.Tags) {
		// 	stopPositions[int64(v.ID)] = v // Store the stop position
		// }
		if isStopPosition(v.Tags, searchTerm) {
			stopPositions[int64(v.ID)] = v
		}
	}

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
					point[0] = nodes[int64(node.ID)].Lon
					point[1] = nodes[int64(node.ID)].Lat
					// the third argumenti is how big the bound is around the line
					localBound := orb.Ring(linebound.GetRotatedBoundWithPad(prevPoint, point, 3))
					trainTracks = append(trainTracks, localBound)
					prevPoint = point
				} else {
					var point orb.Point
					point[0] = nodes[int64(node.ID)].Lon
					point[1] = nodes[int64(node.ID)].Lat
					prevPoint = point
					firstRun = false
				}
			}
		}

		if (v.Tags.Find("railway") == "platform" || v.Tags.Find("public_transport") == "platform") && strings.Contains(v.Tags.Find("name"), searchTerm) {
			platformWays[int64(v.ID)] = v

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
			routes[int64(v.ID)] = v
		}
		if (v.Tags.Find("railway") == "platform" || v.Tags.Find("public_transport") == "platform") && strings.Contains(v.Tags.Find("name"), searchTerm) {
			platformRelations[int64(v.ID)] = v

			// adds to the generic platform list
			platformKey := PlatformKey{Type: osm.TypeRelation, ID: int64(v.ID)}
			platforms[platformKey] = GenericPlatform{
				Services: mapset.NewSet[*osm.Relation](),
			}
		}
	}

	elapsed = time.Since(start)
	log.Printf("Postprocessing took %s", elapsed)

	// Brandenburger Tor Test
	// sourcePlatform := int64(237221908)
	// destPlatform := int64(11762778)

	// Warschauer Strasse Test
	// sourcePlatform := int64(11765307)
	// destPlatform := int64(379339107)

	// Prinzenstraße Test
	sourcePlatform := int64(49038087)
	destPlatform := int64(49038086)

	// Alexanderplatz Test
	// sourcePlatform := int64(3637944)
	// destPlatform := int64(3637412)

	var sourceNodes []int64
	var targetNodes []int64

	// ==================================
	// Match stop positions with services
	// ==================================

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
				if platformID == routeMember.Ref {
					relevantPlatformWays.Add(platform)

					platformKey := PlatformKey{Type: osm.TypeWay, ID: int64(platform.ID)}
					platforms[platformKey].Services.Add(route)

				}
			}
			for platformID, platform := range platformRelations {
				if routeMember.Type == "relation" {

					if platformID == int64(routeMember.ElementID().RelationID()) {
						relevantPlatformRelations.Add(platform)

						platformKey := PlatformKey{Type: osm.TypeRelation, ID: int64(platform.ID)}
						platforms[platformKey].Services.Add(route)
					}
				}
			}
		}
	}
	elapsed = time.Since(start)
	log.Printf("Matching took %s", elapsed)

	platformData := color.New(color.Bold, color.FgWhite).PrintlnFunc()

	for platformKey, genericPlatform := range platforms {
		switch platformKey.Type {
		case osm.TypeWay:
			data := "Platform " + platformWays[platformKey.ID].Tags.Find("name") + " with ID " + fmt.Sprint(platformKey.ID) + " and type " + fmt.Sprint(platformKey.Type) + " has services:"
			platformData(strings.Repeat("=", len(data)))
			platformData(data)
			platformData(strings.Repeat("=", len(data)))
		case osm.TypeRelation:
			data := "Platform " + platformRelations[platformKey.ID].Tags.Find("name") + " with ID " + fmt.Sprint(platformKey.ID) + " and type " + fmt.Sprint(platformKey.Type) + " has services:"
			platformData(strings.Repeat("=", len(data)))
			platformData(data)
			platformData(strings.Repeat("=", len(data)))
		}
		for service := range genericPlatform.Services.Iterator().C {
			printData := service.Tags.Find("name") + " with operator " + service.Tags.Find("operator") + " and vehicle type " + service.Tags.Find("route")
			if len(service.Tags.Find("colour")) == 7 {

				red, err := strconv.ParseInt(service.Tags.Find("colour")[1:3], 16, 16)
				if err != nil {
					fmt.Println("failed decoding line color")
				}
				green, err := strconv.ParseInt(service.Tags.Find("colour")[3:5], 16, 16)
				if err != nil {
					fmt.Println("failed decoding line color")
				}
				blue, err := strconv.ParseInt(service.Tags.Find("colour")[5:7], 16, 16)
				if err != nil {
					fmt.Println("failed decoding line color")
				}
				color.RGB(255, 255, 255).AddBgRGB(int(red), int(green), int(blue)).Println(printData)
			} else {
				fmt.Println(printData)
			}
		}
	}

	fc := orbGeojson.NewFeatureCollection()
	for _, lineString := range trainTracks {
		feature := orbGeojson.NewFeature(lineString)
		fc.Append(feature)
	}
	geojsonData, err2 := json.MarshalIndent(fc, "", "  ")
	if err2 != nil {
		fmt.Println("Error marshalling to GeoJSON")
		return
	}
	file, err4 := os.Create("train_tracks.geojson")
	if err4 != nil {
		fmt.Println("Error creating file")
		return
	}
	defer file.Close()

	// Write the GeoJSON data to the file
	_, err3 := file.Write(geojsonData)
	if err3 != nil {
		fmt.Println("Error writing to file")
		return
	}

	platformWaySpines := make(map[osm.WayID][2]orb.Point)

	for platform := range relevantPlatformWays.Iterator().C {
		platformNodeLength := len(platform.Nodes)
		nodeCloseness := make([]bool, platformNodeLength)

		fmt.Println("Platform: " + fmt.Sprint(platform.Tags.Find("name")) + " with ID: " + fmt.Sprint(platform.ID))
		currentSpine := platformWaySpines[platform.ID]
		if platform.Tags.Find("area") == "yes" {
			for index, node := range platform.Nodes {
				for _, bound := range trainTracks {
					_, err := linebound.IsPointInRectangle(bound, linebound.NodeToPoint(*nodes[int64(node.ID)]))
					isCloseToRails, err := linebound.IsPointInRectangle(bound, linebound.NodeToPoint(*nodes[int64(node.ID)]))
					if err != nil {
						fmt.Println("Failed to check if platform " + fmt.Sprint(platform.ID) + " is inside of bound")
					}
					if isCloseToRails {
						nodeCloseness[index] = isCloseToRails
					} else {
						if nodeCloseness[index] == true {
						} else {
							nodeCloseness[index] = false
						}
					}
				}
			}
			startingPoint := 0
			for index, value := range nodeCloseness {
				if value == false {
					startingPoint = index
					break
				} else {
					fmt.Println("all nodes inside of bounds")
				}
			}
			toMove := nodeCloseness[0:startingPoint]
			slices.Delete(nodeCloseness, 0, startingPoint)
			nodeCloseness = append(nodeCloseness, toMove...)

			platformNodes := make([]osm.WayNode, len(platform.Nodes))
			copy(platformNodes, platform.Nodes)

			platformNodesToMove := platformNodes[0:startingPoint]
			slices.Delete(platformNodes, 0, startingPoint)
			platformNodes = append(platformNodes, platformNodesToMove...)

			longestStart := -1
			longestEnd := -1
			localStart := -1
			localEnd := -1
			for index, value := range nodeCloseness {
				if value {
					if localStart < 0 {
						localStart = index
						localEnd = index
					} else if nodeCloseness[index-1] == false {
						localStart = index
						localEnd = index
					} else {
						localEnd++
					}
				} else {
					if localStart >= 0 {
						if nodeCloseness[index-1] {
							if localEnd-localStart > longestEnd-longestStart {
								longestStart = localStart
								longestEnd = localEnd
							}
						}
					}
				}
			}

			// if longestStart != 0 && longestEnd != 0 {
			relevantNodes := platformNodes[longestStart : longestEnd+1]
			fmt.Println(relevantNodes)

			var spinePoints [2]orb.Point
			firstNode := nodes[int64(relevantNodes[0].ID)]
			lastNode := nodes[int64(relevantNodes[len(relevantNodes)-1].ID)]
			firstPoint := linebound.NodeToPoint(*firstNode)
			lastPoint := linebound.NodeToPoint(*lastNode)
			// fmt.Println("spine points:")
			// fmt.Println("first node id: " + fmt.Sprint(firstNode.ID) + " with lat: " + fmt.Sprint(firstNode.Lat) + " and lon: " + fmt.Sprint(firstNode.Lon))
			spinePoints[0] = firstPoint
			spinePoints[1] = lastPoint

			platformWaySpines[platform.ID] = spinePoints
			// }
		} else {
			startNode := linebound.NodeToPoint(*nodes[int64(platform.Nodes[0].ID)])
			endNode := linebound.NodeToPoint(*nodes[int64(platform.Nodes[len(platform.Nodes)-1].ID)])
			currentSpine[0] = startNode
			currentSpine[1] = endNode
		}
		for _, node := range platform.Nodes {
			if (nodes[int64(node.ID)].Tags.Find("level") != "") || footWays.Contains(node.ID) {
				if int64(platform.ID) == sourcePlatform {
					sourceNodes = append(sourceNodes, int64(node.ID))
				}
				if int64(platform.ID) == destPlatform {
					targetNodes = append(targetNodes, int64(node.ID))
				}
			}
		}

	}
	for platform := range relevantPlatformRelations.Iterator().C {
		for _, member := range platform.Members {
			if member.Type == "way" {
				// get a slice off all the member nodes of a way and range over that
				wayID := member.ElementID().WayID()
				way := ways[int64(wayID)]
				for _, wayNode := range way.Nodes {
					// since way nodes don't have tags i need to find the original node in the map
					node := nodes[int64(wayNode.ID)]
					if (nodes[int64(node.ID)].Tags.Find("level") != "") || footWays.Contains(node.ID) {
						if int64(platform.ID) == sourcePlatform {
							sourceNodes = append(sourceNodes, int64(node.ID))
						}
						if int64(platform.ID) == destPlatform {
							targetNodes = append(targetNodes, int64(node.ID))
						}
					}
				}
			}
		}
	}

	fmt.Println("source nodes:")
	fmt.Println(sourceNodes)
	fmt.Println("target nodes:")
	fmt.Println(targetNodes)

	var shortestPath []graph.Node
	var shortestWeight float64

	for _, sourceID := range sourceNodes {
		// Compute the shortest path tree from the source node
		shortest := path.DijkstraFrom(g.Node(sourceID), g)

		// Extract shortest paths to the destination nodes
		for _, destID := range targetNodes {
			if path, weight := shortest.To(destID); len(path) > 0 {
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
		node := nodes[graphNode.ID()]
		if node.Tags.Find("level") != "" {
			if sourceExitFound == false {
				sourceExitFound = true
				sourceExit = *node
			}
			destExit = *node
		}
	}

	sourceSpine := platformWaySpines[osm.WayID(sourcePlatform)]
	destSpine := platformWaySpines[osm.WayID(destPlatform)]

	sourcePoint0 := linebound.OrbPointToGeoPoint(sourceSpine[0])
	sourcePoint1 := linebound.OrbPointToGeoPoint(sourceSpine[1])

	destPoint0 := linebound.OrbPointToGeoPoint(destSpine[0])
	destPoint1 := linebound.OrbPointToGeoPoint(destSpine[1])

	sourceExitPoint := linebound.OrbPointToGeoPoint(linebound.NodeToPoint(sourceExit))
	destExitPoint := linebound.OrbPointToGeoPoint(linebound.NodeToPoint(destExit))

	sourcePlatformEdgePoint := s2.Project(sourceExitPoint, sourcePoint0, sourcePoint1)
	destPlatformEdgePoint := s2.Project(destExitPoint, destPoint0, destPoint1)

	sourceOptimalDoor := linebound.GeoPointToOrbPoint(sourcePlatformEdgePoint)
	destOptimalDoor := linebound.GeoPointToOrbPoint(destPlatformEdgePoint)
	fmt.Println("optimal spots:")
	fmt.Println(sourceOptimalDoor)
	fmt.Println(destOptimalDoor)

	sourcePlatformLength := geo.DistanceHaversine(sourceSpine[0], sourceSpine[1])
	fromPlatformStart := geo.DistanceHaversine(sourceSpine[0], sourceOptimalDoor)
	destPlatformLength := geo.DistanceHaversine(destSpine[0], destSpine[1])
	toPlatformStart := geo.DistanceHaversine(destSpine[0], destOptimalDoor)

	fmt.Println("platform points:")
	fmt.Println(sourceSpine[0])
	fmt.Println(sourceSpine[1])

	alongSourcePlatform := fromPlatformStart / sourcePlatformLength
	alongDestPlatform := toPlatformStart / destPlatformLength

	fmt.Println("along source platform: " + fmt.Sprint(alongSourcePlatform*100) + "%")
	fmt.Println("along dest platform: " + fmt.Sprint(alongDestPlatform*100) + "%")

	fmt.Println("starting exit: " + fmt.Sprint(sourceExit.ID))
	fmt.Println("ending exit: " + fmt.Sprint(destExit.ID))

	geo := GeoJSON{
		Type: "Feature",
		Geometry: Geometry{
			Type: "LineString",
		},
	}

	for _, node := range shortestPath {
		// Assuming the node ID corresponds to the OSM node ID
		if coord, exists := nodes[node.ID()]; exists {
			geo.Geometry.Coordinates = append(geo.Geometry.Coordinates, []float64{coord.Lon, coord.Lat})
		}
	}

	file, err := os.Create("path.geojson")
	if err != nil {
		fmt.Println("Error creating file:", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(geo); err != nil {
		fmt.Println("Error encoding GeoJSON:", err)
		return
	}

	elapsed = time.Since(start)
	log.Printf("Done in %s", elapsed)

	// Handle any errors that occurred during scanning
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading OSM PBF file: %v", err)
	}
}
