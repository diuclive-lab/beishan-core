// Radix tree for tool routing — optimized prefix matching.
// Absorbed from FangLab internal/tools/radixtree.go (2026-05-26).
package tools

import "strings"

// radixNode is a compressed prefix tree node.
type radixNode struct {
	prefix   string
	value    interface{}
	children []*radixNode
}

// RadixTree is a compressed prefix tree (radix tree) for efficient
// prefix matching and keyword lookup. Thread-safe for reads after
// all inserts are done (insert during init, read during runtime).
type RadixTree struct {
	root *radixNode
	size int
}

// NewRadixTree creates an empty radix tree.
func NewRadixTree() *RadixTree {
	return &RadixTree{root: &radixNode{}}
}

func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// Insert adds a key-value pair to the tree.
func (t *RadixTree) Insert(key string, value interface{}) bool {
	if t == nil || t.root == nil || key == "" {
		return false
	}

	node := t.root
	search := key

	for {
		var child *radixNode
		for _, c := range node.children {
			if c.prefix != "" && c.prefix[0] == search[0] {
				child = c
				break
			}
		}

		if child == nil {
			node.children = appendSorted(node.children, &radixNode{prefix: search, value: value})
			t.size++
			return true
		}

		commonLen := commonPrefixLen(child.prefix, search)

		if commonLen == len(child.prefix) {
			search = search[commonLen:]
			if search == "" {
				if child.value != nil {
					return false
				}
				child.value = value
				t.size++
				return true
			}
			node = child
			continue
		}

		oldSuffix := child.prefix[commonLen:]
		child.prefix = child.prefix[:commonLen]

		newSuffix := search[commonLen:]

		oldChild := &radixNode{
			prefix:   oldSuffix,
			value:    child.value,
			children: child.children,
		}
		child.value = nil
		child.children = []*radixNode{oldChild}

		if newSuffix != "" {
			child.children = appendSorted(child.children, &radixNode{prefix: newSuffix, value: value})
		} else {
			child.value = value
		}
		t.size++
		return true
	}
}

// Match finds the longest prefix match for the given input.
func (t *RadixTree) Match(input string) (value interface{}, matchLen int) {
	if t == nil || t.root == nil || input == "" {
		return nil, 0
	}

	node := t.root
	search := input
	depth := 0

	for {
		var child *radixNode
		for _, c := range node.children {
			if strings.HasPrefix(search, c.prefix) {
				child = c
				break
			}
			if search[0] < c.prefix[0] {
				break
			}
		}

		if child == nil {
			break
		}

		search = search[len(child.prefix):]
		depth += len(child.prefix)

		if child.value != nil {
			value = child.value
			matchLen = depth
		}

		if search == "" {
			break
		}

		node = child
	}

	return value, matchLen
}

// MatchKeyword checks if the input contains any of the tree's keywords.
func (t *RadixTree) MatchKeyword(input string) (value interface{}, matched string) {
	if t == nil || t.root == nil || input == "" {
		return nil, ""
	}
	if t.root.children == nil {
		return nil, ""
	}

	lower := strings.ToLower(strings.TrimSpace(input))

	for start := 0; start < len(lower); start++ {
		c := lower[start]
		hasCandidate := false
		for _, child := range t.root.children {
			if child.prefix != "" && child.prefix[0] == c {
				hasCandidate = true
				break
			}
		}
		if !hasCandidate {
			continue
		}
		val, matchLen := t.Match(lower[start:])
		if val != nil && matchLen > 0 {
			return val, lower[start : start+matchLen]
		}
	}
	return nil, ""
}

// Size returns the number of entries in the tree.
func (t *RadixTree) Size() int {
	if t == nil {
		return 0
	}
	return t.size
}

func appendSorted(children []*radixNode, newNode *radixNode) []*radixNode {
	if newNode == nil || newNode.prefix == "" {
		return children
	}
	pos := 0
	for pos < len(children) {
		if children[pos].prefix != "" && children[pos].prefix[0] >= newNode.prefix[0] {
			break
		}
		pos++
	}
	result := make([]*radixNode, len(children)+1)
	copy(result[:pos], children[:pos])
	result[pos] = newNode
	copy(result[pos+1:], children[pos:])
	return result
}
