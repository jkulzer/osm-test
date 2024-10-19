package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"sync"
	// "github.com/go-chi/chi/v5"
	// "github.com/go-chi/chi/v5/middleware"
	"log"
	"math"
	"os"
	// "strconv"
	"strings"

	// "github.com/jkulzer/osm-test/routes"
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
	}()
	// Open the OSM PBF file
	file, err := os.Open("berlin-latest.osm.pbf")
	if err != nil {
		log.Fatal("Error opening the file:", err)
	}
	defer file.Close()

	// port := 3000

	// Get platforms and print related train services
	getPlatforms(file)
	//
	// fmt.Println("Listening on :" + strconv.Itoa(port))
	// r := chi.NewRouter()
	//
	// r.Use(middleware.Logger)
	//
	// routes.Router(r)
	//
	// http.ListenAndServe(":"+strconv.Itoa(port), r)

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
	platformWays := make(map[int64]*osm.Way)
	platformRelations := make(map[int64]*osm.Relation)

	stopPositions := make(map[int64]*osm.Node)
	stopAreas := make(map[int64]*osm.Relation)
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
			if strings.Contains(v.Tags.Find("name"), searchTerm) {
				rootNode = v
			}
		case *osm.Way:
			ways[int64(v.ID)] = v
			if v.Tags.Find("") == "stop_area" {
				for i := 0; i < len(v.Nodes)-1; i++ {
					g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(v.Nodes[i].ID), simple.Node(v.Nodes[i+1].ID), 1))
					g.SetWeightedEdge(g.NewWeightedEdge(simple.Node(v.Nodes[i+1].ID), simple.Node(v.Nodes[i].ID), 1))
				}
			}
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
		}
	}

	// Collect routes from relations
	for _, v := range relations {
		if v.Tags.Find("type") == "route" {
			routes[int64(v.ID)] = v
		}
		if (v.Tags.Find("railway") == "platform" || v.Tags.Find("public_transport") == "platform") && strings.Contains(v.Tags.Find("name"), searchTerm) {
			platformRelations[int64(v.ID)] = v
		}
	}

	var sourceNodes []int64
	var targetNodes []int64

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
			for platformID, platform := range platformWays {
				if platformID == routeMember.Ref {
					fmt.Printf(
						"Platform %d has Service %s and is Way\n",
						platform.ID,
						route.Tags.Find("name"),
					)
					for _, node := range platform.Nodes {
						if platformID == 237221908 {
							sourceNodes = append(sourceNodes, int64(node.ID))
						}
						if platformID == 383076366 {
							targetNodes = append(targetNodes, int64(node.ID))
						}
						fmt.Println("  ID: " + fmt.Sprint(node.ID))
					}
				}
			}
			for platformID, platform := range platformRelations {
				// if routeMember.Type == osm.TypeWay && platformID == routeMember.Ref {
				if platformID == routeMember.Ref {
					fmt.Printf(
						"Platform %d has Service %s and is Relation\n",
						platform.ID,
						route.Tags.Find("name"),
					)
				}
			}
		}
	}

	i := 0

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
	for _, _ = range ways {
		i++
		// fmt.Println("Way with ID " + fmt.Sprint(way.ID) + " and name " + way.Tags.Find("name"))
	}
	fmt.Println("Number of ways: " + fmt.Sprint(i))

	// route := path.DijkstraFrom(simple.Node(5234612423), g)
	//
	// fmt.Println(route.To(5234612350))
	var wg sync.WaitGroup
	shortestPathChan := make(chan []graph.Node)
	minDistanceChan := make(chan float64)

	for _, source := range sourceNodes {
		wg.Add(1)
		go func(source int64) {
			defer wg.Done()
			for _, target := range targetNodes {
				paths := path.DijkstraFrom(g.Node(source), g)
				pathToTarget, distance := paths.To(target)
				minDistanceChan <- distance
				shortestPathChan <- pathToTarget
			}
		}(source)
	}

	go func() {
		wg.Wait()
		close(shortestPathChan)
		close(minDistanceChan)
	}()

	minDistance := math.Inf(1)
	var shortestPath []graph.Node
	for dist := range minDistanceChan {
		if dist < minDistance {
			minDistance = dist
			shortestPath = <-shortestPathChan
		}
	}

	fmt.Println("Shortest path")
	fmt.Println(shortestPath)

	// Handle any errors that occurred during scanning
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading OSM PBF file: %v", err)
	}
}

func getPlatformEdges() {

}
