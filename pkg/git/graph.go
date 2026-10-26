package git

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Graph3DNode struct {
	Hash         string
	ShortHash    string
	ParentHashes []string
	AuthorName   string
	Date         time.Time
	Message      string
	Branch       string
	IsMerge      bool
	Lane         int
	Order        int
	Refs         []string
}

type Graph3DEdge struct {
	FromHash string
	ToHash   string
	IsMerge  bool
	FromLane int
	ToLane   int
}

type Graph3DData struct {
	Nodes      []Graph3DNode
	Edges      []Graph3DEdge
	NodeByHash map[string]int
}

// GetGraph3D retrieves commit topological DAG for 3D branch & merge visualization
func (s *Service) GetGraph3D(repoPath string, limit int) (*Graph3DData, error) {
	if limit <= 0 {
		limit = 120
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := s.runGit(ctx, repoPath, "log", "--all", "--topo-order",
		"--format=%H%x1f%h%x1f%P%x1f%an%x1f%at%x1f%s%x1f%D",
		fmt.Sprintf("-n%d", limit),
	)
	if err != nil {
		return nil, err
	}

	data := &Graph3DData{
		NodeByHash: make(map[string]int),
	}

	currentBranch, _ := s.GetCurrentBranch(repoPath)
	if currentBranch == "" {
		currentBranch = "master"
	}

	var rawNodes []Graph3DNode
	scanner := bufio.NewScanner(strings.NewReader(out))
	order := 0
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\x1f")
		if len(parts) < 6 {
			continue
		}
		hash := parts[0]
		shortHash := parts[1]
		var parents []string
		if strings.TrimSpace(parts[2]) != "" {
			parents = strings.Split(strings.TrimSpace(parts[2]), " ")
		}
		author := parts[3]
		epoch, _ := strconv.ParseInt(parts[4], 10, 64)
		msg := parts[5]
		refStr := ""
		if len(parts) >= 7 {
			refStr = parts[6]
		}

		var refs []string
		branchName := ""
		if refStr != "" {
			rawRefs := strings.Split(refStr, ",")
			for _, r := range rawRefs {
				r = strings.TrimSpace(r)
				if r != "" {
					refs = append(refs, r)
					if strings.Contains(r, "->") {
						rParts := strings.Split(r, "->")
						if len(rParts) == 2 {
							branchName = strings.TrimSpace(rParts[1])
						}
					} else if !strings.HasPrefix(r, "tag:") && branchName == "" {
						branchName = strings.TrimPrefix(r, "origin/")
					}
				}
			}
		}

		rawNodes = append(rawNodes, Graph3DNode{
			Hash:         hash,
			ShortHash:    shortHash,
			ParentHashes: parents,
			AuthorName:   author,
			Date:         time.Unix(epoch, 0),
			Message:      SanitizeCodeText(msg),
			Branch:       branchName,
			IsMerge:      len(parents) > 1,
			Order:        order,
			Refs:         refs,
		})
		order++
	}

	if len(rawNodes) == 0 {
		return data, nil
	}

	laneMap := make(map[string]int)
	laneMap[currentBranch] = 0
	laneMap["master"] = 0
	laneMap["main"] = 0
	nextLane := 1

	commitLane := make(map[string]int)

	for i := range rawNodes {
		n := &rawNodes[i]
		if n.Branch != "" {
			if lane, exists := laneMap[n.Branch]; exists {
				commitLane[n.Hash] = lane
			} else {
				laneMap[n.Branch] = nextLane
				commitLane[n.Hash] = nextLane
				nextLane++
			}
		}
	}

	for i := range rawNodes {
		n := &rawNodes[i]
		lane, hasLane := commitLane[n.Hash]
		if !hasLane {
			lane = 0
			commitLane[n.Hash] = 0
		}
		n.Lane = lane

		for pIdx, pHash := range n.ParentHashes {
			if _, pHasLane := commitLane[pHash]; !pHasLane {
				if pIdx == 0 {
					commitLane[pHash] = lane
				} else {
					commitLane[pHash] = nextLane
					nextLane++
				}
			}
		}
	}

	for i := range rawNodes {
		rawNodes[i].Lane = commitLane[rawNodes[i].Hash]
		data.NodeByHash[rawNodes[i].Hash] = i
	}
	data.Nodes = rawNodes

	for _, n := range data.Nodes {
		for pIdx, pHash := range n.ParentHashes {
			if pNodeIdx, exists := data.NodeByHash[pHash]; exists {
				pNode := data.Nodes[pNodeIdx]
				data.Edges = append(data.Edges, Graph3DEdge{
					FromHash: n.Hash,
					ToHash:   pHash,
					IsMerge:  pIdx > 0,
					FromLane: n.Lane,
					ToLane:   pNode.Lane,
				})
			}
		}
	}

	return data, nil
}
