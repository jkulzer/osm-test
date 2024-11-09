package linebound

import (
	"errors"
	"fmt"
	"github.com/golang/geo/s2"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geo"
	"github.com/paulmach/osm"
	"math"
	"slices"
)

func GetRotatedBoundWithPad(p1 orb.Point, p2 orb.Point, d float64) orb.LineString {
	var lineString orb.LineString
	lineBearing := geo.Bearing(p1, p2)
	var bearingUp float64
	var bearingDown float64
	if lineBearing < 90 {
		bearingUp = lineBearing + 90
		bearingDown = lineBearing + 270
	} else {
		bearingUp = lineBearing + 90
		bearingDown = lineBearing - 90
	}
	n1 := geo.PointAtBearingAndDistance(p1, bearingUp, d)
	n2 := geo.PointAtBearingAndDistance(p1, bearingDown, d)
	n3 := geo.PointAtBearingAndDistance(p2, bearingDown, d)
	n4 := geo.PointAtBearingAndDistance(p2, bearingUp, d)

	lineString = append(lineString, n1)
	lineString = append(lineString, n2)
	lineString = append(lineString, n3)
	lineString = append(lineString, n4)
	lineString = append(lineString, n1)

	return lineString
}

func IsPointInRectangle(ring orb.Ring, point orb.Point) (bool, error) {
	if len(ring) != 5 || !ring.Closed() {
		return false, errors.New("test") // Not a valid rectangle ring
	}

	for i := 0; i < 4; i++ {
		a := ring[i]
		b := ring[i+1]

		// Calculate the vector from point a to b
		edge := orb.Point{b[0] - a[0], b[1] - a[1]}
		// Calculate the vector from point a to the test point
		toPoint := orb.Point{point[0] - a[0], point[1] - a[1]}

		// Compute the cross product of edge and toPoint vectors
		crossProduct := edge[0]*toPoint[1] - edge[1]*toPoint[0]
		if crossProduct > 0 {
			return false, nil // Point is outside the rectangle
		}
	}
	return true, nil
}

func NodeToPoint(node osm.Node) orb.Point {
	var point orb.Point
	point[0] = node.Lon
	point[1] = node.Lat
	return point
}

func WayNodeToPoint(wayNode osm.WayNode) orb.Point {
	var point orb.Point
	point[0] = wayNode.Lon
	point[1] = wayNode.Lat
	return point
}

func OrbPointToGeoPoint(point orb.Point) s2.Point {
	return s2.PointFromLatLng(s2.LatLngFromDegrees(point.Lat(), point.Lon()))
}

func OsmNodeToGeoPoint(node osm.Node) s2.Point {
	return s2.PointFromLatLng(s2.LatLngFromDegrees(node.Lat, node.Lon))
}

func GeoPointToOrbPoint(point s2.Point) orb.Point {
	// Convert s2.Point to s2.LatLng
	latLng := s2.LatLngFromPoint(point)

	// Convert latitude and longitude from radians to degrees
	lat := radiansToDegrees(latLng.Lat.Radians())
	lng := radiansToDegrees(latLng.Lng.Radians())

	// Return orb.Point in (longitude, latitude) format
	return orb.Point{lng, lat}
}

func radiansToDegrees(rad float64) float64 {
	return rad * 180.0 / math.Pi
}

func GetPlatformSpine(sourceNodes []osm.Node, platformSpines map[osm.ElementID][2]orb.Point, trainTracks []orb.Ring, nodes map[osm.NodeID]*osm.Node, elementID osm.ElementID) {
	platformNodeLength := len(sourceNodes)
	nodeCloseness := make([]bool, platformNodeLength)

	for index, node := range sourceNodes {
		for _, bound := range trainTracks {
			_, err := IsPointInRectangle(bound, NodeToPoint(*nodes[node.ID]))
			isCloseToRails, err := IsPointInRectangle(bound, NodeToPoint(*nodes[node.ID]))
			if err != nil {
				fmt.Println("Failed to check if platform " + fmt.Sprint(elementID) + " is inside of bound")
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
			// all nodes inside of bounds
		}
	}
	toMove := nodeCloseness[0:startingPoint]
	slices.Delete(nodeCloseness, 0, startingPoint)
	nodeCloseness = append(nodeCloseness, toMove...)

	platformNodes := make([]osm.WayNode, len(sourceNodes))
	copy(platformNodes, platformNodes)

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

	var spinePoints [2]orb.Point
	firstNode := sourceNodes[0]
	lastNode := sourceNodes[platformNodeLength-1]
	if firstNode.ID == lastNode.ID {
		firstPoint := NodeToPoint(firstNode)
		lastPoint := NodeToPoint(lastNode)
		spinePoints[0] = firstPoint
		spinePoints[1] = lastPoint
	} else {
		spinePoints[0] = NodeToPoint(firstNode)
		spinePoints[1] = NodeToPoint(lastNode)
	}

	platformSpines[elementID] = spinePoints
}
