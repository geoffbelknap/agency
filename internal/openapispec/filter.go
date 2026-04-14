package openapispec

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

var methodKeys = map[string]struct{}{
	"get":     {},
	"post":    {},
	"put":     {},
	"patch":   {},
	"delete":  {},
	"options": {},
	"head":    {},
	"trace":   {},
}

// Load reads the first available spec from the provided candidate paths.
func Load(paths []string) ([]byte, error) {
	var errs []string
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			return data, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", p, err))
	}
	return nil, fmt.Errorf("openapi spec not found (%s)", strings.Join(errs, "; "))
}

// FilterByTier returns a generated YAML view that contains only operations in
// the requested tier. Tier resolution prefers operation-level x-agency-tier
// metadata and falls back to the tagged group tier.
func FilterByTier(data []byte, tier string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("openapi spec is empty")
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("openapi document must be a mapping")
	}

	tagTiers := extractTagTiers(doc)
	usedTags := map[string]struct{}{}

	pathsNode := mappingValue(doc, "paths")
	if pathsNode != nil && pathsNode.Kind == yaml.MappingNode {
		filteredPaths := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for i := 0; i < len(pathsNode.Content); i += 2 {
			pathKey := cloneNode(pathsNode.Content[i])
			pathValue := pathsNode.Content[i+1]
			if pathValue.Kind != yaml.MappingNode {
				continue
			}

			filteredPathValue := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			hasMethod := false
			for j := 0; j < len(pathValue.Content); j += 2 {
				opKey := pathValue.Content[j]
				opValue := pathValue.Content[j+1]
				method := strings.ToLower(opKey.Value)
				if _, ok := methodKeys[method]; !ok {
					filteredPathValue.Content = append(filteredPathValue.Content, cloneNode(opKey), cloneNode(opValue))
					continue
				}
				if !operationMatchesTier(opValue, tagTiers, tier, usedTags) {
					continue
				}
				hasMethod = true
				filteredPathValue.Content = append(filteredPathValue.Content, cloneNode(opKey), cloneNode(opValue))
			}

			if hasMethod {
				filteredPaths.Content = append(filteredPaths.Content, pathKey, filteredPathValue)
			}
		}
		setMappingValue(doc, "paths", filteredPaths)
	}

	tagsNode := mappingValue(doc, "tags")
	if tagsNode != nil && tagsNode.Kind == yaml.SequenceNode {
		filteredTags := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, tagItem := range tagsNode.Content {
			tagName := scalarValue(mappingValue(tagItem, "name"))
			if _, ok := usedTags[tagName]; ok {
				filteredTags.Content = append(filteredTags.Content, cloneNode(tagItem))
			}
		}
		setMappingValue(doc, "tags", filteredTags)
	}

	setTopLevelViewMetadata(doc, tier)

	out, err := yaml.Marshal(&root)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func setTopLevelViewMetadata(doc *yaml.Node, tier string) {
	setMappingValue(doc, "x-agency-generated-view", scalarNode(tier))
	infoNode := mappingValue(doc, "info")
	if infoNode == nil || infoNode.Kind != yaml.MappingNode {
		return
	}
	descriptionNode := mappingValue(infoNode, "description")
	if descriptionNode == nil || descriptionNode.Kind != yaml.ScalarNode {
		return
	}
	description := strings.TrimRight(descriptionNode.Value, "\n")
	suffix := fmt.Sprintf("\n\nGenerated API view: %s.", tier)
	if !strings.Contains(description, suffix) {
		descriptionNode.Value = description + suffix
	}
}

func operationMatchesTier(opNode *yaml.Node, tagTiers map[string]string, tier string, usedTags map[string]struct{}) bool {
	if opNode == nil || opNode.Kind != yaml.MappingNode {
		return false
	}

	opTier := scalarValue(mappingValue(opNode, "x-agency-tier"))
	if opTier == "" {
		for _, tag := range sequenceScalars(mappingValue(opNode, "tags")) {
			if tagTier := tagTiers[tag]; tagTier != "" {
				opTier = tagTier
				break
			}
		}
	}
	if opTier == "" {
		opTier = "core"
	}
	if opTier != tier {
		return false
	}

	for _, tag := range sequenceScalars(mappingValue(opNode, "tags")) {
		usedTags[tag] = struct{}{}
	}
	return true
}

func extractTagTiers(doc *yaml.Node) map[string]string {
	tagTiers := map[string]string{}
	tagsNode := mappingValue(doc, "tags")
	if tagsNode == nil || tagsNode.Kind != yaml.SequenceNode {
		return tagTiers
	}
	for _, tagItem := range tagsNode.Content {
		name := scalarValue(mappingValue(tagItem, "name"))
		tier := scalarValue(mappingValue(tagItem, "x-agency-tier"))
		if name != "" && tier != "" {
			tagTiers[name] = tier
		}
	}
	return tagTiers
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func setMappingValue(node *yaml.Node, key string, value *yaml.Node) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content, scalarNode(key), value)
}

func scalarValue(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

func sequenceScalars(node *yaml.Node) []string {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	values := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		if item.Kind == yaml.ScalarNode {
			values = append(values, item.Value)
		}
	}
	return values
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func cloneNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	cloned := *node
	if len(node.Content) > 0 {
		cloned.Content = make([]*yaml.Node, len(node.Content))
		for i, child := range node.Content {
			cloned.Content[i] = cloneNode(child)
		}
	}
	return &cloned
}
