// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package graph

import (
	"fmt"

	"github.com/tejzpr/mimir-mcp/internal/database"
)

// GraphNode represents a node in the memory graph
type GraphNode struct {
	MemoryID uint
	Slug     string
	Title    string
	Depth    int
}

// GraphEdge represents an edge in the memory graph
type GraphEdge struct {
	SourceID        uint
	TargetID        uint
	AssociationType string
	Strength        float64
}

// Graph represents a memory graph
type Graph struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

// TraverseGraph performs a graph traversal from a starting memory
func (m *Manager) TraverseGraph(startMemoryID uint, maxHops int, breadthFirst bool) (*Graph, error) {
	if maxHops > 5 {
		maxHops = 5 // Safety limit
	}

	graph := &Graph{
		Nodes: []GraphNode{},
		Edges: []GraphEdge{},
	}

	visited := make(map[uint]bool)
	
	if breadthFirst {
		return m.traverseBFS(startMemoryID, maxHops, graph, visited)
	}
	return m.traverseDFS(startMemoryID, maxHops, 0, graph, visited)
}

// traverseBFS performs breadth-first traversal
func (m *Manager) traverseBFS(startID uint, maxHops int, graph *Graph, visited map[uint]bool) (*Graph, error) {
	type queueItem struct {
		memoryID uint
		depth    int
	}

	queue := []queueItem{{startID, 0}}
	visited[startID] = true

	// Add start node
	if err := m.addNodeToGraph(startID, 0, graph); err != nil {
		return nil, err
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth >= maxHops {
			continue
		}

		// Get associations
		associations, err := m.GetAssociations(current.memoryID)
		if err != nil {
			continue
		}

		for _, assoc := range associations {
			// Determine neighbor ID
			neighborID := assoc.TargetMemoryID
			if assoc.SourceMemoryID != current.memoryID {
				neighborID = assoc.SourceMemoryID
			}

			// Add edge
			graph.Edges = append(graph.Edges, GraphEdge{
				SourceID:        assoc.SourceMemoryID,
				TargetID:        assoc.TargetMemoryID,
				AssociationType: assoc.AssociationType,
				Strength:        assoc.Strength,
			})

			// Visit neighbor if not visited
			if !visited[neighborID] {
				visited[neighborID] = true
				if err := m.addNodeToGraph(neighborID, current.depth+1, graph); err != nil {
					continue
				}
				queue = append(queue, queueItem{neighborID, current.depth + 1})
			}
		}
	}

	return graph, nil
}

// traverseDFS performs depth-first traversal
func (m *Manager) traverseDFS(memoryID uint, maxHops, currentDepth int, graph *Graph, visited map[uint]bool) (*Graph, error) {
	if currentDepth >= maxHops {
		return graph, nil
	}

	if visited[memoryID] {
		return graph, nil
	}

	visited[memoryID] = true

	// Add node
	if err := m.addNodeToGraph(memoryID, currentDepth, graph); err != nil {
		return graph, err
	}

	// Get associations
	associations, err := m.GetAssociations(memoryID)
	if err != nil {
		return graph, err
	}

	for _, assoc := range associations {
		// Add edge
		graph.Edges = append(graph.Edges, GraphEdge{
			SourceID:        assoc.SourceMemoryID,
			TargetID:        assoc.TargetMemoryID,
			AssociationType: assoc.AssociationType,
			Strength:        assoc.Strength,
		})

		// Determine neighbor ID
		neighborID := assoc.TargetMemoryID
		if assoc.SourceMemoryID != memoryID {
			neighborID = assoc.SourceMemoryID
		}

		// Recursively traverse
		if !visited[neighborID] {
			_, _ = m.traverseDFS(neighborID, maxHops, currentDepth+1, graph, visited)
		}
	}

	return graph, nil
}

// addNodeToGraph adds a memory node to the graph
func (m *Manager) addNodeToGraph(memoryID uint, depth int, graph *Graph) error {
	var mem database.MimirMemory
	if err := m.db.First(&mem, memoryID).Error; err != nil {
		return fmt.Errorf("failed to get memory: %w", err)
	}

	graph.Nodes = append(graph.Nodes, GraphNode{
		MemoryID: memoryID,
		Slug:     mem.Slug,
		Title:    mem.Title,
		Depth:    depth,
	})

	return nil
}
