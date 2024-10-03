package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"

	"github.com/RyanCarrier/dijkstra/v2"
	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"
	"strings"
)

type DNode struct {
	ID    int64
	Paths []DPath
}

type DPath struct {
	NextNode int64
	Weight   int
}

// Check if the element has tags indicating it is a train platform with the given name
func isTrainPlatformWithName(tags osm.Tags, name string) bool {
	return (tags.Find("railway") == "platform" || tags.Find("public_transport") == "platform") && strings.Contains(tags.Find("name"), name)
}

func isStopPosition(tags osm.Tags, name string) bool {
	return tags.Find("public_transport") == "stop_position" && strings.Contains(tags.Find("name"), name)
}

// Check if a stop position has a route
func isPartOfRoute(tags osm.Tags) bool {
	return tags.Find("public_transport") == "stop_position" && tags.Find("route") != ""
}

func isPath(tags osm.Tags) bool {
	return tags.Find("highway") == "steps" || tags.Find("tunnel") == "building_passage"
}

func isRoute(tags osm.Tags, name string) bool {
	if name == "" {
		return true
	} else {
		return strings.Contains(tags.Find("name"), name)
	}
}

func isPlatformWay(tags osm.Tags) bool {
	return tags.Find("indoor") == "yes"
}

func isWithinStationRadius(v *osm.Node, startLat float64, startLon float64) bool {
	// Earth's radius in meters
	const EarthRadius = 6371000

	// Simplified 2D distance between two coordinates
	// Convert degrees to radians
	lat1Rad := v.Lat * math.Pi / 180
	lon1Rad := v.Lon * math.Pi / 180
	lat2Rad := startLat * math.Pi / 180
	lon2Rad := startLon * math.Pi / 180

	// Calculate the differences
	dLat := lat2Rad - lat1Rad
	dLon := lon2Rad - lon1Rad

	// Apply the Haversine formula
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	// Compute the distance in meters
	distance := EarthRadius * c

	// fmt.Println(distance)
	if distance < 500 {
		return true
	} else {
		return false
	}
}

func wayIsWithinStationRadius(v *osm.Way, start *osm.Relation) bool {
	// Earth's radius in meters
	const EarthRadius = 6371000

	exampleWayNode := v.Nodes[0]
	exampleStartNode := start.Members[0]

	// Simplified 2D distance between two coordinates
	// Convert degrees to radians
	lat1Rad := exampleWayNode.Lat * math.Pi / 180
	lon1Rad := exampleWayNode.Lon * math.Pi / 180
	lat2Rad := exampleStartNode.Lat * math.Pi / 180
	lon2Rad := exampleStartNode.Lon * math.Pi / 180

	// Calculate differences
	dLat := lat2Rad - lat1Rad
	dLon := (lon2Rad - lon1Rad) * math.Cos(lat1Rad)

	// Compute simple 2D distance
	distance := EarthRadius * math.Sqrt(dLat*dLat+dLon*dLon)

	if distance < 500 {
		return true
	} else {
		return false
	}
}

func main() {
	// Open the OSM PBF file
	file, err := os.Open("berlin-latest.osm.pbf")
	if err != nil {
		log.Fatal("Error opening the file:", err)
	}
	defer file.Close()

	// Get platforms and print related train services
	getPlatforms(file)
}

func getPlatforms(file *os.File) {

	searchTerm := "Brandenburger Tor"

	// variable declarations
	nodes := make(map[int64]*osm.Node)
	ways := make(map[int64]*osm.Way)
	relations := make(map[int64]*osm.Relation)

	platforms := make(map[int64]*osm.Way)
	stopPositions := make(map[int64]*osm.Node)
	routes := make(map[int64]*osm.Relation)
	paths := make(map[int64]*osm.Way)

	// dijkstra
	graph := dijkstra.NewGraph()
	var starterRelation *osm.Relation

	// Create a PBF reader
	scanner := osmpbf.New(context.Background(), file, 4)

forLoop:
	for scanner.Scan() {
		// Get the next OSM object
		obj := scanner.Object()

		switch v := obj.(type) {
		case *osm.Node:
			nodes[int64(v.ID)] = v
		case *osm.Way:
			ways[int64(v.ID)] = v
		case *osm.Relation:
			relations[int64(v.ID)] = v
			if strings.Contains(v.Tags.Find("name"), searchTerm) && v.Tags.Find("public_transport") == "stop_area" {
				starterRelation = v
				fmt.Println("Starter node is:" + v.Tags.Find("name"))
				break forLoop
			}
		default:
			// Handle other OSM object types if needed
		}
	}

	for _, v := range nodes {
		if isWithinStationRadius(v, 52.516480, 13.381224) {
			graph.AddEmptyVertex(int(v.ID))
			fmt.Println(v.ID)
		}
		// Check if the node is a train stop position
		if isPartOfRoute(v.Tags) {
			stopPositions[int64(v.ID)] = v // Store the stop position
		}
		if isStopPosition(v.Tags, "Brandenburger Tor") {
			stopPositions[int64(v.ID)] = v
		}
	}
	for _, v := range ways {
		if isTrainPlatformWithName(v.Tags, searchTerm) {
			// fmt.Printf("Found train platform (Way): ID=%d, Tags=%v\n", v.ID, v.Tags)
			platforms[int64(v.ID)] = v // Store the platform
		}
		if isPath(v.Tags) {
			// fmt.Printf("Found path (Way): ID=%d, Tags=%v\n", v.ID, v.Tags)
			paths[int64(v.ID)] = v
			// graph.AddArc(src, test, 3)
		}
		if wayIsWithinStationRadius(v, starterRelation) {
			graph.AddArc(int(v.Nodes[0].ID), int(v.Nodes[0].ID), 5)
		}
	}
	// for _, v := range paths {
	// }
	for _, v := range relations {

		// Check if the relation represents a train platform
		if isTrainPlatformWithName(v.Tags, searchTerm) {
			// fmt.Printf("Found train platform (Relation): ID=%d, Tags=%v\n", v.ID, v.Tags)
		}
		if isRoute(v.Tags, "") {
			routes[int64(v.ID)] = v
		}
	}

	for stopID, stopPosition := range stopPositions {
		for _, route := range routes {
			for _, routeMember := range route.Members {
				if routeMember.Type == "node" {
					if stopID == routeMember.Ref {
						fmt.Println(
							"Stop: " + stopPosition.Tags.Find("name") +
								" with service " + route.Tags.Find("name") +
								" on platform " + stopPosition.Tags.Find("local_ref"),
						)
					}
				}
			}
		}
	}

	fmt.Println(graph.Shortest(2400549248, 2400549255))

	// Handle any errors that occurred during scanning
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading OSM PBF file: %v", err)
	}
}
