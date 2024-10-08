package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"strings"

	"github.com/paulmach/osm"
	"github.com/paulmach/osm/osmpbf"
	"gonum.org/v1/gonum/graph/path"
	"gonum.org/v1/gonum/graph/simple"
)

type DNode struct {
	ID    int64
	Paths []DPath
}

type DPath struct {
	NextNode int64
	Weight   int
}

func wayIsWithinDistance(centerNode *osm.Node, targetWay osm.Way) bool {
	return math.Abs(centerNode.Lat-targetWay.Nodes[1].Lat) < 0.005 && math.Abs(centerNode.Lon-targetWay.Nodes[1].Lon) < 0.005
}

func isWithinDistance(centerNode *osm.Node, targetNode osm.Node) bool {
	return math.Abs(centerNode.Lat-targetNode.Lat) < 0.001 && math.Abs(centerNode.Lon-targetNode.Lon) < 0.005
}

func isStopPosition(tags osm.Tags, name string) bool {
	return tags.Find("public_transport") == "stop_position" && strings.Contains(tags.Find("name"), name)
}

// Check if a stop position has a route
func isPartOfRoute(tags osm.Tags) bool {
	return tags.Find("public_transport") == "stop_position" && tags.Find("route") != ""
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

	// var rootRelation *osm.Relation
	var rootNode *osm.Node

	// Maps for storing OSM data
	nodes := make(map[int64]*osm.Node)
	ways := make(map[int64]*osm.Way)
	relations := make(map[int64]*osm.Relation)
	relevantNodes := make(map[int64]*osm.Node)
	platforms := make(map[int64]*osm.Way)

	stopPositions := make(map[int64]*osm.Node)
	stopAreas := make(map[int64]*osm.Relation)
	routes := make(map[int64]*osm.Relation)

	// Create a PBF reader
	scanner := osmpbf.New(context.Background(), file, 4)

	// Scan and populate the relations map
	for scanner.Scan() {
		// Get the next OSM object
		obj := scanner.Object()

		switch v := obj.(type) {
		case *osm.Node:
			nodes[int64(v.ID)] = v
			if strings.Contains(v.Tags.Find("name"), searchTerm) {
				rootNode = v
			}
		case *osm.Way:
			ways[int64(v.ID)] = v
		case *osm.Relation:
			relations[int64(v.ID)] = v
			// if strings.Contains(v.Tags.Find("name"), searchTerm) && v.Tags.Find("public_transport") == "stop_area" {
			if strings.Contains(v.Tags.Find("name"), searchTerm) {
				stopAreas[int64(v.ID)] = v
				fmt.Println(v.Tags.Find("name"))
				// rootRelation = v
			}
		default:
			// Handle other OSM object types if needed
		}
	}

	fmt.Println("Root Node has ID " + fmt.Sprint(rootNode.ID))

	// Filter nodes for stop positions
	for _, v := range nodes {
		relevantNodes[int64(v.ID)] = v
		if isWithinDistance(rootNode, *v) {
		}
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
			platforms[int64(v.ID)] = v
		}
	}

	// Collect routes from relations
	for _, v := range relations {
		if v.Tags.Find("type") == "route" {
			routes[int64(v.ID)] = v
		}
	}

	// Match stop positions with services
	for _, route := range routes {
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
			for platformID, platform := range platforms {
				if routeMember.Type == osm.TypeWay && platformID == routeMember.Ref {
					fmt.Printf(
						"Platform %d has Service %s\n",
						platform.ID,
						route.Tags.Find("name"),
					)
				}
			}
		}
	}

	i := 0
	g := simple.NewWeightedDirectedGraph(1, 0)

	for _, node := range relevantNodes {
		i++
		// fmt.Println("Node with ID " + fmt.Sprint(node.ID) + " and name " + node.Tags.Find("name"))
		g.AddNode(simple.Node(node.ID))
		if node.ID == 2400549248 || node.ID == 2400549255 {
			// fmt.Println("GOTTEM")
		}
	}
	fmt.Println("Number of nodes: " + fmt.Sprint(i))

	i = 0
	for _, way := range ways {
		i++
		// fmt.Println("Way with ID " + fmt.Sprint(way.ID) + " and name " + way.Tags.Find("name"))
		for i := 0; i < len(way.Nodes)-1; i++ {
			g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(way.Nodes[i].ID), simple.Node(way.Nodes[i+1].ID), 1))
			g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(way.Nodes[i+1].ID), simple.Node(way.Nodes[i].ID), 1))
		}
	}
	fmt.Println("Number of ways: " + fmt.Sprint(i))

	route := path.DijkstraFrom(simple.Node(5234612350), g)

	fmt.Println(route.To(253846340))

	// route = path.DijkstraFrom(simple.Node(3876345194), g)
	//
	// fmt.Println(route.To(10769657204))

	// Example Dijkstra shortest path calculation
	// fmt.Println(graph.Shortest(2400549248, 2400549255))

	// Handle any errors that occurred during scanning
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading OSM PBF file: %v", err)
	}
}
