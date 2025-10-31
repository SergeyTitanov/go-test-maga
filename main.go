package main

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <yaml-file>\n", os.Args[0])
		os.Exit(1)
	}
	filePath := os.Args[1]
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing YAML: %v\n", err)
		os.Exit(1)
	}

	// Determine root mapping node
	var mapping *yaml.Node
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		mapping = root.Content[0]
	} else {
		mapping = &root
	}

	var errs []string

	// Find spec node and validate fields
	specNode := findMapKey(mapping, "spec")
	if specNode != nil && specNode.Kind == yaml.MappingNode {
		// Validate spec.os
		errs = append(errs, validateOS(specNode, filePath)...)

		// Validate each container in spec.containers
		conts := findMapKey(specNode, "containers")
		if conts != nil && conts.Kind == yaml.SequenceNode {
			for _, contNode := range conts.Content {
				if contNode.Kind != yaml.MappingNode {
					continue
				}
				// readinessProbe.httpGet.port validation
				errs = append(errs, validateHTTPGetPort(contNode, filePath)...)
				// resources.requests.cpu validation
				errs = append(errs, validateCPU(contNode, filePath)...)
			}
		}
	}

	// Print errors to stderr
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, e)
	}
	if len(errs) > 0 {
		os.Exit(1)
	}
}

func findMapKey(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	// Mapping node Content has [key0, val0, key1, val1, ...]
	for i := 0; i < len(node.Content); i += 2 {
		k := node.Content[i]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func validateOS(specNode *yaml.Node, filename string) []string {
	var errs []string
	osNode := findMapKey(specNode, "os")
	if osNode != nil && osNode.Kind == yaml.ScalarNode {
		val := osNode.Value
		if val != "linux" && val != "windows" {
			errs = append(errs, fmt.Sprintf("%s:%d os has unsupported value '%s'", filename, osNode.Line, val))
		}
	}
	return errs
}

func validateHTTPGetPort(contNode *yaml.Node, filename string) []string {
	var errs []string
	rpNode := findMapKey(contNode, "readinessProbe")
	if rpNode != nil && rpNode.Kind == yaml.MappingNode {
		httpGetNode := findMapKey(rpNode, "httpGet")
		if httpGetNode != nil && httpGetNode.Kind == yaml.MappingNode {
			portNode := findMapKey(httpGetNode, "port")
			if portNode != nil && portNode.Kind == yaml.ScalarNode {
				// Parse port as int and check range
				portVal, err := strconv.Atoi(portNode.Value)
				if err != nil || portVal < 1 || portVal > 65535 {
					errs = append(errs, fmt.Sprintf("%s:%d port value out of range", filename, portNode.Line))
				}
			}
		}
	}
	return errs
}

func validateCPU(contNode *yaml.Node, filename string) []string {
	var errs []string
	resNode := findMapKey(contNode, "resources")
	if resNode != nil && resNode.Kind == yaml.MappingNode {
		reqNode := findMapKey(resNode, "requests")
		if reqNode != nil && reqNode.Kind == yaml.MappingNode {
			cpuNode := findMapKey(reqNode, "cpu")
			if cpuNode != nil && cpuNode.Kind == yaml.ScalarNode {
				if cpuNode.Tag != "!!int" {
					errs = append(errs, fmt.Sprintf("%s:%d cpu must be int", filename, cpuNode.Line))
				}
			}
		}
	}
	return errs
}
