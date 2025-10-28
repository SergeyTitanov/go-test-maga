package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

func main() {
	// Ensure exactly one argument (the YAML file path) is provided
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: yamlvalid <path/to/file.yaml>")
		os.Exit(1)
	}
	filename := os.Args[1]

	// Read the file content
	data, err := os.ReadFile(filename)
	if err != nil {
		// If file can't be read, print error and exit
		fmt.Fprintf(os.Stderr, "%s:0 %v\n", filename, err)
		os.Exit(1)
	}

	// Parse YAML into a yaml.Node to preserve structure and line numbers
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		// Extract line number from YAML error if possible
		errMsg := err.Error()
		lineNum := 0
		// yaml.v3 error messages often contain "line X:" – try to find it
		var parseLine int
		if n, _ := fmt.Sscanf(errMsg, "yaml: line %d:", &parseLine); n == 1 {
			lineNum = parseLine
		}
		// Print parse error with file:line
		fmt.Fprintf(os.Stderr, "%s:%d %s\n", filename, lineNum, errMsg)
		os.Exit(1)
	}

	// If the root node is a document node, get its content (actual mapping)
	var rootMap *yaml.Node
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		rootMap = root.Content[0]
	} else {
		rootMap = &root
	}
	// We expect the root of the YAML to be a mapping (Pod spec as an object)
	if rootMap.Kind != yaml.MappingNode {
		fmt.Fprintf(os.Stderr, "%s:1 YAML content is not a mapping\n", filename)
		os.Exit(1)
	}

	var errors []string

	// Helper function to append formatted error
	addError := func(line int, msg string) {
		errors = append(errors, fmt.Sprintf("%s:%d %s", filename, line, msg))
	}

	// Helper to find a mapping node by key within a mapping node
	findMapKey := func(mapNode *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
		if mapNode == nil || mapNode.Kind != yaml.MappingNode {
			return nil, nil
		}
		// Content in a mapping node is [keyNode, valueNode, keyNode, valueNode, ...]
		for i := 0; i < len(mapNode.Content); i += 2 {
			k := mapNode.Content[i]
			v := mapNode.Content[i+1]
			if k.Value == key {
				return k, v
			}
		}
		return nil, nil
	}

	// Top-level required fields: apiVersion, kind, metadata.name
	// apiVersion
	if _, apiVersionNode := findMapKey(rootMap, "apiVersion"); apiVersionNode == nil {
		// Missing apiVersion, use line 1 (top of file) as we don't have a specific line
		addError(1, "apiVersion is required")
	}
	// kind
	if _, kindNode := findMapKey(rootMap, "kind"); kindNode == nil {
		addError(1, "kind is required")
	}
	// metadata and metadata.name
	var metadataNode *yaml.Node
	if _, mNode := findMapKey(rootMap, "metadata"); mNode == nil {
		// If metadata block missing entirely
		addError(1, "metadata.name is required")
	} else {
		metadataNode = mNode
		if metadataNode.Kind != yaml.MappingNode {
			// If metadata is not a mapping, treat as missing name
			addError(metadataNode.Line, "metadata.name is required")
		} else {
			if _, nameNode := findMapKey(metadataNode, "name"); nameNode == nil || nameNode.Value == "" {
				// If name missing or empty
				addError(metadataNode.Line, "metadata.name is required")
			}
			// We don't validate namespace or labels content specifically (they are optional)
		}
	}

	// spec
	var specNode *yaml.Node
	if _, sNode := findMapKey(rootMap, "spec"); sNode == nil {
		// If spec missing, then containers definitely missing
		addError(1, "spec.containers is required")
	} else {
		specNode = sNode
		if specNode.Kind != yaml.MappingNode {
			addError(specNode.Line, "spec.containers is required")
		} else {
			// spec.os (if present, must be "linux" or "windows")
			if osKey, osVal := findMapKey(specNode, "os"); osVal != nil {
				// Only valid if "linux" or "windows"
				if osVal.Value != "linux" && osVal.Value != "windows" {
					addError(osVal.Line, "spec.os must be 'linux' or 'windows'")
				}
			}
			// spec.containers (required)
			var containersNode *yaml.Node
			if _, cNode := findMapKey(specNode, "containers"); cNode == nil {
				addError(specNode.Line, "spec.containers is required")
			} else {
				containersNode = cNode
				if containersNode.Kind != yaml.SequenceNode {
					addError(containersNode.Line, "spec.containers must be a sequence (list)")
				} else {
					if len(containersNode.Content) == 0 {
						// containers list is empty
						addError(containersNode.Line, "spec.containers must have at least one container")
					} else {
						// Validate each container entry
						for _, container := range containersNode.Content {
							if container.Kind != yaml.MappingNode {
								addError(container.Line, "container must be a mapping object")
								// skip further checks on this item
								continue
							}
							// Each container mapping
							// name (required, snake_case)
							if nameKey, nameVal := findMapKey(container, "name"); nameVal == nil || nameVal.Value == "" {
								// If name missing or empty
								// If nameKey exists but no value (unlikely in YAML), treat as missing
								line := container.Line
								if nameKey != nil {
									line = nameKey.Line
								}
								addError(line, "container name is required")
							} else {
								// Check snake_case: lowercase letters, digits, underscores
								match, _ := regexp.MatchString(`^[a-z0-9]+(?:_[a-z0-9]+)*$`, nameVal.Value)
								if !match {
									addError(nameVal.Line, "container name must be snake_case (lowercase with underscores)")
								}
							}
							// image (required, with prefix and tag)
							if imageKey, imageVal := findMapKey(container, "image"); imageVal == nil || imageVal.Value == "" {
								line := container.Line
								if imageKey != nil {
									line = imageKey.Line
								}
								addError(line, "image is required")
							} else {
								// Validate image string
								// Must start with registry.bigbrother.io/ and contain a colon for tag
								if len(imageVal.Value) < len("registry.bigbrother.io/") ||
									imageVal.Value[:len("registry.bigbrother.io/")] != "registry.bigbrother.io/" ||
									!containsColonAfterPrefix(imageVal.Value, "registry.bigbrother.io/") {
									addError(imageVal.Line, "image must start with 'registry.bigbrother.io/' and contain a tag (use ':')")
								}
							}
							// ports (if present)
							if _, portsVal := findMapKey(container, "ports"); portsVal != nil {
								if portsVal.Kind != yaml.SequenceNode {
									addError(portsVal.Line, "ports must be a list")
								} else {
									for _, portItem := range portsVal.Content {
										if portItem.Kind != yaml.MappingNode {
											addError(portItem.Line, "port must be a mapping (with containerPort and protocol)")
											continue
										}
										// Each port mapping
										if cpKey, cpVal := findMapKey(portItem, "containerPort"); cpVal == nil {
											line := portItem.Line
											if cpKey != nil {
												line = cpKey.Line
											}
											addError(line, "containerPort is required")
										} else {
											// Check containerPort is int 1..65535
											if cpVal.Tag != "!!int" {
												// If not an int in YAML (e.g. quoted or something), try parsing as int
												if _, err := strconv.Atoi(cpVal.Value); err != nil {
													addError(cpVal.Line, "containerPort must be an integer between 1 and 65535")
												}
											}
											// In any case, attempt to parse
											if portNum, err := strconv.Atoi(cpVal.Value); err == nil {
												if portNum < 1 || portNum > 65535 {
													addError(cpVal.Line, "containerPort must be between 1 and 65535")
												}
											} else {
												// Non-integer already handled above
											}
										}
										if protoKey, protoVal := findMapKey(portItem, "protocol"); protoVal != nil {
											// Protocol given, must be TCP or UDP
											if protoVal.Value != "TCP" && protoVal.Value != "UDP" {
												addError(protoVal.Line, "protocol must be either TCP or UDP")
											}
										}
									}
								}
							}
							// readinessProbe (if present)
							if _, probeNode := findMapKey(container, "readinessProbe"); probeNode != nil {
								validateProbe(filename, &errors, "readinessProbe", probeNode, addError, findMapKey)
							}
							// livenessProbe (if present)
							if _, probeNode := findMapKey(container, "livenessProbe"); probeNode != nil {
								validateProbe(filename, &errors, "livenessProbe", probeNode, addError, findMapKey)
							}
							// resources (if present)
							if _, resNode := findMapKey(container, "resources"); resNode != nil {
								checkResourceValues(filename, &errors, resNode, addError, findMapKey)
							}
						}
					}
				}
			}
		}
	}

	// If any errors were collected, print them to stderr and exit 1
	if len(errors) > 0 {
		for _, e := range errors {
			fmt.Fprintln(os.Stderr, e)
		}
		os.Exit(1)
	}
	// If no errors, exit 0 (implicitly, as program will end).
	os.Exit(0)
}

// containsColonAfterPrefix checks if there is a ':' after the given prefix in the string.
func containsColonAfterPrefix(s string, prefix string) bool {
	if len(s) <= len(prefix) {
		return false
	}
	rest := s[len(prefix):]
	// Check that there's a colon in the remaining string, and not at the very start or end of it
	colonIndex := -1
	for i, ch := range rest {
		if ch == ':' {
			colonIndex = i
			break
		}
	}
	if colonIndex <= 0 || colonIndex == len(rest)-1 {
		return false
	}
	return true
}

// validateProbe checks the httpGet path and port for readiness/liveness probes.
func validateProbe(filename string, errs *[]string, probeName string, probeNode *yaml.Node,
	addError func(line int, msg string), findMapKey func(*yaml.Node, string) (*yaml.Node, *yaml.Node)) {
	if probeNode.Kind != yaml.MappingNode {
		addError(probeNode.Line, fmt.Sprintf("%s must be a mapping", probeName))
		return
	}
	// Only support httpGet probes in this validation
	_, httpGetNode := findMapKey(probeNode, "httpGet")
	if httpGetNode == nil {
		// If no httpGet, we skip validation (assuming other types not supported by this tool)
		return
	}
	if httpGetNode.Kind != yaml.MappingNode {
		addError(httpGetNode.Line, fmt.Sprintf("%s.httpGet must be a mapping", probeName))
		return
	}
	// path
	if pathKey, pathVal := findMapKey(httpGetNode, "path"); pathVal == nil || pathVal.Value == "" {
		line := httpGetNode.Line
		if pathKey != nil {
			line = pathKey.Line
		}
		addError(line, fmt.Sprintf("%s.httpGet.path is required", probeName))
	} else {
		if len(pathVal.Value) == 0 || pathVal.Value[0] != '/' {
			addError(pathVal.Line, fmt.Sprintf("%s.httpGet.path must start with '/'", probeName))
		}
	}
	// port
	if portKey, portVal := findMapKey(httpGetNode, "port"); portVal == nil {
		line := httpGetNode.Line
		if portKey != nil {
			line = portKey.Line
		}
		addError(line, fmt.Sprintf("%s.httpGet.port is required", probeName))
	} else {
		// Check port is int 1..65535
		if portVal.Tag != "!!int" {
			if _, err := strconv.Atoi(portVal.Value); err != nil {
				addError(portVal.Line, fmt.Sprintf("%s.httpGet.port must be an integer between 1 and 65535", probeName))
			}
		}
		if pNum, err := strconv.Atoi(portVal.Value); err == nil {
			if pNum < 1 || pNum > 65535 {
				addError(portVal.Line, fmt.Sprintf("%s.httpGet.port must be between 1 and 65535", probeName))
			}
		}
	}
}

// checkResourceValues validates the resources.limits and resources.requests sections.
func checkResourceValues(filename string, errs *[]string, resourcesNode *yaml.Node,
	addError func(line int, msg string), findMapKey func(*yaml.Node, string) (*yaml.Node, *yaml.Node)) {
	if resourcesNode.Kind != yaml.MappingNode {
		addError(resourcesNode.Line, "resources must be a mapping")
		return
	}
	// Define a regex for memory values: one or more digits + optional unit (Ki,Mi,... or K,M,...)
	memoryPattern := regexp.MustCompile(`^[0-9]+(?:Ki|Mi|Gi|Ti|Pi|Ei|K|M|G|T|P|E)$`)
	// Check both "limits" and "requests"
	for _, resType := range []string{"limits", "requests"} {
		if _, resNode := findMapKey(resourcesNode, resType); resNode == nil {
			addError(resourcesNode.Line, fmt.Sprintf("resources.%s is required", resType))
		} else if resNode.Kind != yaml.MappingNode {
			addError(resNode.Line, fmt.Sprintf("resources.%s must be a mapping", resType))
		} else {
			// cpu
			if cpuKey, cpuVal := findMapKey(resNode, "cpu"); cpuVal == nil || cpuVal.Value == "" {
				line := resNode.Line
				if cpuKey != nil {
					line = cpuKey.Line
				}
				addError(line, fmt.Sprintf("resources.%s.cpu is required", resType))
			} else {
				// CPU must be int >= 0
				// Attempt to parse as integer
				i, err := strconv.Atoi(cpuVal.Value)
				if err != nil {
					addError(cpuVal.Line, fmt.Sprintf("resources.%s.cpu must be a non-negative integer", resType))
				} else if i < 0 {
					addError(cpuVal.Line, fmt.Sprintf("resources.%s.cpu must be a non-negative integer", resType))
				}
			}
			// memory
			if memKey, memVal := findMapKey(resNode, "memory"); memVal == nil || memVal.Value == "" {
				line := resNode.Line
				if memKey != nil {
					line = memKey.Line
				}
				addError(line, fmt.Sprintf("resources.%s.memory is required", resType))
			} else {
				// Memory must match the pattern (number + unit)
				if !memoryPattern.MatchString(memVal.Value) {
					addError(memVal.Line, fmt.Sprintf("resources.%s.memory must be in format <number><unit> (e.g., 123Mi or 1Gi)", resType))
				}
			}
		}
	}
}
