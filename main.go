package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/RyanCarrier/dijkstra/v2"
	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"
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
	}
	return strings.Contains(tags.Find("name"), name)
}

func isPlatformWay(tags osm.Tags) bool {
	return tags.Find("indoor") == "yes"
}

// Process the relation recursively and collect all nodes and ways
func processRelation(r *osm.Relation, nodes map[int64]*osm.Node, ways map[int64]*osm.Way, relations map[int64]*osm.Relation) {
	fmt.Println("Root relation: " + fmt.Sprint(r.ID))
	for _, member := range r.Members {
		fmt.Println("Member:")
		fmt.Println(member)

		switch member.Type {
		case "node":
			if node, exists := nodes[member.Ref]; exists {
				// Store node if found
				nodes[int64(node.ID)] = node
			}
		case "way":
			if way, exists := ways[member.Ref]; exists {
				// Store the way if found
				ways[int64(way.ID)] = way

				// Process the nodes that are part of the way
				for _, wayNode := range way.Nodes {
					if node, exists := nodes[int64(wayNode.ID)]; exists {
						// Store the node if found
						nodes[int64(node.ID)] = node
					}
				}
			}
		case "relation":
			if rel, exists := relations[member.Ref]; exists {
				// Store the relation and recursively process nested relations
				relations[int64(rel.ID)] = rel
				processRelation(rel, nodes, ways, relations)
			}
		}
	}
}

func main() {
	// Open the OSM PBF file
	file, err := os.Open("berlin-latest.osm")
	if err != nil {
		log.Fatal("Error opening the file:", err)
	}
	defer file.Close()

	// Get platforms and print related train services
	getPlatforms(file)
}

func getPlatforms(file *os.File) {
	searchTerm := "U Brandenburger Tor"

	var rootRelation *osm.Relation

	// Maps for storing OSM data
	nodes := make(map[int64]*osm.Node)
	ways := make(map[int64]*osm.Way)
	relations := make(map[int64]*osm.Relation)

	platforms := make(map[int64]*osm.Way)
	stopPositions := make(map[int64]*osm.Node)
	stopAreas := make(map[int64]*osm.Relation)
	routes := make(map[int64]*osm.Relation)
	paths := make(map[int64]*osm.Way)

	// Dijkstra graph
	graph := dijkstra.NewGraph()

	// Create a PBF reader
	scanner := osmpbf.New(context.Background(), file, 4)

	// Scan and populate the relations map
	for scanner.Scan() {
		// Get the next OSM object
		obj := scanner.Object()

		switch v := obj.(type) {
		case *osm.Relation:
			relations[int64(v.ID)] = v
			// if strings.Contains(v.Tags.Find("name"), searchTerm) && v.Tags.Find("public_transport") == "stop_area" {
			if strings.Contains(v.Tags.Find("name"), searchTerm) {
				stopAreas[int64(v.ID)] = v
				fmt.Println(v.Tags.Find("name"))
				rootRelation = v
			}
		default:
			// Handle other OSM object types if needed
		}
	}
	processRelation(rootRelation, nodes, ways, relations)

	// Process nodes and ways associated with each relation
	// for _, r := range relations {
	// }

	// Filter nodes for stop positions
	for _, v := range nodes {
		if isPartOfRoute(v.Tags) {
			stopPositions[int64(v.ID)] = v // Store the stop position
		}
		if isStopPosition(v.Tags, searchTerm) {
			stopPositions[int64(v.ID)] = v
		}
	}

	// Filter ways for platforms and paths
	for _, v := range ways {
		if isTrainPlatformWithName(v.Tags, searchTerm) {
			platforms[int64(v.ID)] = v // Store the platform
		}
		if isPath(v.Tags) {
			paths[int64(v.ID)] = v
		}
	}

	// Collect routes from relations
	for _, v := range relations {
		if isRoute(v.Tags, "") {
			routes[int64(v.ID)] = v
		}
	}

	// Display stop areas and members
	for _, stopArea := range stopAreas {
		fmt.Println("Stop area:", stopArea.Tags.Find("name"))
	}

	// Match stop positions with services
	for stopID, stopPosition := range stopPositions {
		for _, route := range routes {
			for _, routeMember := range route.Members {
				if routeMember.Type == osm.TypeNode && stopID == routeMember.Ref {
					fmt.Printf(
						"Stop: %s with service %s on platform %s\n",
						stopPosition.Tags.Find("name"),
						route.Tags.Find("name"),
						stopPosition.Tags.Find("local_ref"),
					)
				}
			}
		}
	}

	for _, node := range nodes {
		fmt.Println("Node with ID " + fmt.Sprint(node.ID) + " and name " + node.Tags.Find("name"))
	}

	// Example Dijkstra shortest path calculation
	fmt.Println(graph.Shortest(2400549248, 2400549255))

	// Handle any errors that occurred during scanning
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading OSM PBF file: %v", err)
	}
}

