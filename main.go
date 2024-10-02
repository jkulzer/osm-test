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

// Check if a stop position has a route
func isPartOfRoute(tags osm.Tags) bool {
	return tags.Find("public_transport") == "stop_position" && tags.Find("route") != ""
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
	searchTerm := "Brandenburger Tor"          // Change this to the platform you're interested in
	platforms := make(map[int64]*osm.Way)      // Store found platforms
	stopPositions := make(map[int64]*osm.Node) // Store found stop positions

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
		case *osm.Way:
			// Check if the way represents a train platform
			if isTrainPlatformWithName(v.Tags, searchTerm) {
				fmt.Printf("Found train platform (Way): ID=%d, Tags=%v\n", v.ID, v.Tags)
				platforms[int64(v.ID)] = v // Store the platform
			}
		case *osm.Relation:
			// Check if the relation represents a train platform
			if isTrainPlatformWithName(v.Tags, searchTerm) {
				fmt.Printf("Found train platform (Relation): ID=%d, Tags=%v\n", v.ID, v.Tags)
			}
		default:
			// Handle other OSM object types if needed
		}
	}

	// Handle any errors that occurred during scanning
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading OSM PBF file: %v", err)
	}
}
