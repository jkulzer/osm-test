package main

import (
	"testing"

	"context"
	"github.com/jkulzer/platform-router/linebound"
	"github.com/jkulzer/platform-router/ui"
	"gonum.org/v1/gonum/graph/simple"

	"github.com/jkulzer/osm"
	"github.com/jkulzer/osm/osmpbf"
	"github.com/paulmach/orb/geo"

	"os"
)

func BenchmarkShortestPathBetweenArrayOfNodes(b *testing.B) {
	nodes := make(map[osm.NodeID]*osm.Node)
	ways := make(map[osm.WayID]*osm.Way)
	relations := make(map[osm.RelationID]*osm.Relation)
	file, err := os.Open("./berlin-latest.osm.pbf")
	if err != nil {
		panic(err)
	}

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
			for i, _ := range v.Nodes {

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
			}
		case *osm.Relation:
			relations[v.ID] = v
		default:
			// Handle other OSM object types if needed
		}
	}
	loadingContainer := ui.NewLoadingScreenWithTextWidget()
	sourceNodes := []osm.NodeID{osm.NodeID(2451641844), osm.NodeID(4170056703), osm.NodeID(4170056702), osm.NodeID(12330904367), osm.NodeID(10846473246)}
	destNodes := []osm.NodeID{osm.NodeID(4170056704), osm.NodeID(2400549269), osm.NodeID(5063750065), osm.NodeID(2400549255)}
	shortestPathBetweenArrayOfNodes(sourceNodes, destNodes, g, loadingContainer)
}
