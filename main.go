package main

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// validateProbe checks a readiness/liveness probe node for an invalid port value.
func validateProbe(filename string, errs *[]string, probeNode *yaml.Node) {
	if probeNode == nil || probeNode.Kind != yaml.MappingNode {
		return
	}
	// Find the httpGet mapping in the probe
	for i := 0; i < len(probeNode.Content); i += 2 {
		keyNode := probeNode.Content[i]
		valNode := probeNode.Content[i+1]
		if keyNode.Value == "httpGet" && valNode.Kind == yaml.MappingNode {
			// Within httpGet, find the port field
			for j := 0; j < len(valNode.Content); j += 2 {
				k := valNode.Content[j]
				v := valNode.Content[j+1]
				if k.Value == "port" && v.Kind == yaml.ScalarNode {
					// Try to interpret the port value as an integer
					portStr := v.Value
					if portStr != "" {
						if port, err := strconv.Atoi(portStr); err == nil {
							// If it's a number, check the valid range (1-65535)
							if port < 1 || port > 65535 {
								*errs = append(*errs, fmt.Sprintf("%s:%d port value out of range", filename, v.Line))
							}
						}
						// If the port value is not a number (e.g., a named port like "http"),
						// we do not treat it as an error in this context.
					}
				}
			}
		}
	}
}

// checkResourceValues validates resource request values (e.g., CPU) in a resources node.
func checkResourceValues(filename string, errs *[]string, resourcesNode *yaml.Node) {
	if resourcesNode == nil || resourcesNode.Kind != yaml.MappingNode {
		return
	}
	// Find the requests mapping within resources
	for i := 0; i < len(resourcesNode.Content); i += 2 {
		keyNode := resourcesNode.Content[i]
		valNode := resourcesNode.Content[i+1]
		if keyNode.Value == "requests" && valNode.Kind == yaml.MappingNode {
			// Look for the "cpu" field in requests
			for j := 0; j < len(valNode.Content); j += 2 {
				reqKey := valNode.Content[j]
				reqVal := valNode.Content[j+1]
				if reqKey.Value == "cpu" && reqVal.Kind == yaml.ScalarNode {
					// If the CPU request is provided as a string (quoted number)
					if reqVal.Tag == "!!str" {
						*errs = append(*errs, fmt.Sprintf("%s:%d cpu must be int", filename, reqVal.Line))
					}
				}
			}
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: yamlvalidator <file.yaml>")
		os.Exit(1)
	}
	filename := os.Args[1]
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read file: %v\n", err)
		os.Exit(1)
	}
	// Parse the YAML into a Node tree
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		fmt.Fprintf(os.Stderr, "YAML parse error: %v\n", err)
		os.Exit(1)
	}
	// Navigate to the document root (which should be a mapping for the Pod spec)
	var doc *yaml.Node
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		doc = root.Content[0]
	} else {
		doc = &root
	}
	if doc.Kind != yaml.MappingNode {
		fmt.Fprintf(os.Stderr, "%s: root YAML node is not a mapping\n", filename)
		os.Exit(1)
	}

	var errs []string

	// Find the "spec" node in the top-level mapping
	var specNode *yaml.Node
	for i := 0; i < len(doc.Content); i += 2 {
		keyNode := doc.Content[i]
		valNode := doc.Content[i+1]
		if keyNode.Value == "spec" {
			specNode = valNode
			break
		}
	}
	if specNode != nil && specNode.Kind == yaml.MappingNode {
		// 1. Validate spec.os if present
		for i := 0; i < len(specNode.Content); i += 2 {
			keyNode := specNode.Content[i]
			valNode := specNode.Content[i+1]
			if keyNode.Value == "os" && valNode.Kind == yaml.ScalarNode {
				// If os is specified as a string, check value
				if valNode.Value != "linux" && valNode.Value != "windows" {
					errs = append(errs, fmt.Sprintf("%s:%d os has unsupported value '%s'", filename, valNode.Line, valNode.Value))
				}
			}
		}
		// 2. Validate readinessProbe.httpGet.port and 3. resources.requests.cpu for each container
		for i := 0; i < len(specNode.Content); i += 2 {
			keyNode := specNode.Content[i]
			valNode := specNode.Content[i+1]
			if keyNode.Value == "containers" && valNode.Kind == yaml.SequenceNode {
				// Iterate over the list of containers
				for _, containerNode := range valNode.Content {
					if containerNode.Kind != yaml.MappingNode {
						continue
					}
					// Check each container's fields for readinessProbe and resources
					for j := 0; j < len(containerNode.Content); j += 2 {
						cKey := containerNode.Content[j]
						cVal := containerNode.Content[j+1]
						if cKey.Value == "readinessProbe" {
							validateProbe(filename, &errs, cVal)
						}
						if cKey.Value == "resources" {
							checkResourceValues(filename, &errs, cVal)
						}
					}
				}
			}
		}
	}

	// Output errors if any, and set exit code
	if len(errs) > 0 {
		for _, msg := range errs {
			fmt.Fprintln(os.Stderr, msg)
		}
		os.Exit(1)
	}
	os.Exit(0)
}
