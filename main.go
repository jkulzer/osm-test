package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	// "github.com/go-chi/chi/v5"
	// "github.com/go-chi/chi/v5/middleware"
	mapset "github.com/deckarep/golang-set/v2"
	"log"
	"os"
	// "strconv"
	"strings"

	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"
	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/path"
	"gonum.org/v1/gonum/graph/simple"
)

func isStopPosition(tags osm.Tags, name string) bool {
	return tags.Find("public_transport") == "stop_position" && strings.Contains(tags.Find("name"), name)
}

// Check if a stop position has a route
func isPartOfRoute(tags osm.Tags) bool {
	return tags.Find("public_transport") == "stop_position" && tags.Find("route") != ""
}

func main() {
	go func() {
		http.ListenAndServe("localhost:6060", nil)
	}() // Open the OSM PBF file
	file, err := os.Open("berlin-notlatest.osm.pbf")
	if err != nil {
		log.Fatal("Error opening the file:", err)
	}
	defer file.Close()

	// Get platforms and print related train services
	getPlatforms(file)

}

func getPlatforms(file *os.File) {
	searchTerm := "Warschauer Straße"

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
					g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(v.Nodes[i+1].ID), simple.Node(v.Nodes[i].ID), 1))
					g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(v.Nodes[i].ID), simple.Node(v.Nodes[i+1].ID), 1))
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
			fmt.Println("platform way: " + v.Tags.Find("name") + " with ID " + fmt.Sprint(v.ID))
		}
	}

	// Collect routes from relations
	for _, v := range relations {
		if v.Tags.Find("type") == "route" {
			routes[int64(v.ID)] = v
		}
		if (v.Tags.Find("railway") == "platform" || v.Tags.Find("public_transport") == "platform") && strings.Contains(v.Tags.Find("name"), searchTerm) {
			platformRelations[int64(v.ID)] = v
			fmt.Println("platform relation: " + v.Tags.Find("name") + " with ID " + fmt.Sprint(v.ID))
		}
	}

	sourcePlatform := int64(379339107)
	destPlatform := int64(11765307)

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
					fmt.Printf(
						"Platform %d has Service %s and is Way\n",
						platform.ID,
						route.Tags.Find("name"),
					)
					relevantPlatformWays.Add(platform)
				}
			}
			for platformID, platform := range platformRelations {
				if routeMember.Type == "relation" {

					if platformID == int64(routeMember.ElementID().RelationID()) {
						fmt.Printf(
							"Platform %d has Service %s and is Relation\n",
							platform.ID,
							route.Tags.Find("name"),
						)
						relevantPlatformRelations.Add(platform)
					}
				}
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

	// route := path.DijkstraFrom(simple.Node(sourceNodes[0]), g)
	//
	// fmt.Println(route.To(targetNodes[0]))

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
