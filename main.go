package main

import (
	"context"
	"fmt"
	"github.com/fatih/color"
	"math"
	"net/http"
	_ "net/http/pprof"
	"strconv"
	// "github.com/go-chi/chi/v5"
	// "github.com/go-chi/chi/v5/middleware"
	mapset "github.com/deckarep/golang-set/v2"
	"log"
	"os"
	"strings"

	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"
	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/path"
	"gonum.org/v1/gonum/graph/simple"
	// "fyne.io/fyne/v2/app"
	// "fyne.io/fyne/v2/widget"
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
	const earthRadius = 6371 // Earth's radius in kilometers
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
	return earthRadius * c
}

func getPlatforms(file *os.File) {
	searchTerm := "Brandenburger Tor"

	// Maps for storing OSM data
	nodes := make(map[int64]*osm.Node)
	ways := make(map[int64]*osm.Way)
	relations := make(map[int64]*osm.Relation)
	relevantNodes := make(map[int64]*osm.Node)
	platformWays := make(map[int64]*osm.Way)
	platformRelations := make(map[int64]*osm.Relation)
	relevantPlatformWays := mapset.NewSet[*osm.Way]()
	relevantPlatformRelations := mapset.NewSet[*osm.Relation]()
	footWays := mapset.NewSet[osm.NodeID]()

	platforms := make(map[PlatformKey]GenericPlatform)

	stopPositions := make(map[int64]*osm.Node)
	routes := make(map[int64]*osm.Relation)

	// Create a PBF reader
	scanner := osmpbf.New(context.Background(), file, 4)
	g := simple.NewWeightedDirectedGraph(1, 0)

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
						fmt.Println()
						// disable routing through elevators
						// if thisNode.Tags.Find("highway") == "elevator" || nextNode.Tags.Find("highway") == "elevator" {
						// } else {

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
						// }
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

	// Filter nodes for stop positions
	for _, v := range nodes {
		relevantNodes[int64(v.ID)] = v
		// if isPartOfRoute(v.Tags) {
		// 	stopPositions[int64(v.ID)] = v // Store the stop position
		// }
		if isStopPosition(v.Tags, searchTerm) {
			stopPositions[int64(v.ID)] = v
		}
	}

	// Filter ways for platforms and paths
	for _, v := range ways {
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

	sourcePlatform := int64(237221908)
	destPlatform := int64(11762778)
	// Warschauer Strasse test
	// sourcePlatform := int64(11765307)
	// destPlatform := int64(379339107)

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
			// 			"Stop: %s with service %s on platform %s\n",
			// 			stopPosition.Tags.Find("name"),
			// 			route.Tags.Find("name"),
			// 			stopPosition.Tags.Find("local_ref"),
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

	for platformKey, genericPlatform := range platforms {
		switch platformKey.Type {
		case osm.TypeWay:
			fmt.Println("Platform " + platformWays[platformKey.ID].Tags.Find("name") + " with ID " + fmt.Sprint(platformKey.ID) + " and type " + fmt.Sprint(platformKey.Type) + " has services:")
		case osm.TypeRelation:
			fmt.Println("Platform " + platformRelations[platformKey.ID].Tags.Find("name") + " with ID " + fmt.Sprint(platformKey.ID) + " and type " + fmt.Sprint(platformKey.Type) + " has services:")
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

	for platform := range relevantPlatformWays.Iterator().C {
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

	// Handle any errors that occurred during scanning
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading OSM PBF file: %v", err)
	}
}
