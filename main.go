package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"
	"strings"
)

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

func isRoute(tags osm.Tags, route_type string, name string) bool {
	// return tags.Find("route") == route_type &&
	return true
	return strings.Contains(tags.Find("name"), name)
}

func isPlatformWay(tags osm.Tags) bool {
	return tags.Find("indoor") == "yes"
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
	platforms := make(map[int64]*osm.Way)
	stopPositions := make(map[int64]*osm.Node)
	routes := make(map[int64]*osm.Relation)
	ways := make(map[int64]*osm.Way)
	paths := make(map[int64]*osm.Way)
	// nodes := make(map[int64]*osm.Node)

	// Create a PBF reader
	scanner := osmpbf.New(context.Background(), file, 4)

	// Iterate through the OSM objects
	for scanner.Scan() {
		// Get the next OSM object
		obj := scanner.Object()

		switch v := obj.(type) {
		case *osm.Node:
			// Check if the node is a train stop position
			if isPartOfRoute(v.Tags) {
				stopPositions[int64(v.ID)] = v // Store the stop position
			}
			if isStopPosition(v.Tags, "Brandenburger Tor") {
				// fmt.Printf("Found stop position (Node): ID=%d, Tags=%v\n", v.ID, v.Tags)
				stopPositions[int64(v.ID)] = v
			}
		case *osm.Way:
			// Check if the way represents a train platform
			if isTrainPlatformWithName(v.Tags, searchTerm) {
				fmt.Printf("Found train platform (Way): ID=%d, Tags=%v\n", v.ID, v.Tags)
				platforms[int64(v.ID)] = v // Store the platform
			}
			if isPlatformWay(v.Tags) {
				ways[int64(v.ID)] = v // Store the way
			}
			if isPath(v.Tags) {
				// fmt.Printf("Found path (Way): ID=%d, Tags=%v\n", v.ID, v.Tags)
				paths[int64(v.ID)] = v
			}
		case *osm.Relation:
			// Check if the relation represents a train platform
			if isTrainPlatformWithName(v.Tags, searchTerm) {
				// fmt.Printf("Found train platform (Relation): ID=%d, Tags=%v\n", v.ID, v.Tags)
			}
			if isRoute(v.Tags, "", "") {
				// fmt.Printf("Found route (Relation): ID=%d, Tags=%v\n", v.ID, v.Tags)
				routes[int64(v.ID)] = v
			}
		default:
			// Handle other OSM object types if needed
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

	// Handle any errors that occurred during scanning
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading OSM PBF file: %v", err)
	}
}
