package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// getMappingValue возвращает узел-значение для заданного ключа в YAML-узле типа Mapping, либо nil, если ключ не найден.
func getMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		k := node.Content[i]
		v := node.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return v
		}
	}
	return nil
}

// validateMappingField получает значение поля `field` из родительского узла-объекта `parent`.
// Если поле обязательное (required) и отсутствует, добавляет сообщение об ошибке. Возвращает узел значения или nil.
func validateMappingField(parent *yaml.Node, field string, required bool, errors *[]string, filename string) *yaml.Node {
	valNode := getMappingValue(parent, field)
	if valNode == nil && required {
		*errors = append(*errors, fmt.Sprintf("%s: %s is required", filename, field))
	}
	return valNode
}

// validateAPIVersion проверяет, что узел apiVersion является скаляром со значением "v1".
func validateAPIVersion(node *yaml.Node, errors *[]string, filename string) {
	if node == nil {
		return
	}
	if node.Kind != yaml.ScalarNode {
		*errors = append(*errors, fmt.Sprintf("%s:%d apiVersion must be string", filename, node.Line))
	} else if node.Value != "v1" {
		*errors = append(*errors, fmt.Sprintf("%s:%d apiVersion has unsupported value '%s'", filename, node.Line, node.Value))
	}
}

// validateKind проверяет, что узел kind является скаляром со значением "Pod".
func validateKind(node *yaml.Node, errors *[]string, filename string) {
	if node == nil {
		return
	}
	if node.Kind != yaml.ScalarNode {
		*errors = append(*errors, fmt.Sprintf("%s:%d kind must be string", filename, node.Line))
	} else if node.Value != "Pod" {
		*errors = append(*errors, fmt.Sprintf("%s:%d kind has unsupported value '%s'", filename, node.Line, node.Value))
	}
}

// validateMetadata проверяет узел metadata (должен быть объектом) и его поля:
// name (обязательное, непустая строка), namespace (необязательное, строка) и labels (необязательное, объект с строковыми ключами/значениями).
func validateMetadata(metaNode *yaml.Node, errors *[]string, filename string) {
	if metaNode == nil {
		return
	}
	if metaNode.Kind != yaml.MappingNode {
		*errors = append(*errors, fmt.Sprintf("%s:%d metadata must be object", filename, metaNode.Line))
		return
	}
	nameNode := validateMappingField(metaNode, "name", true, errors, filename)
	nsNode := validateMappingField(metaNode, "namespace", false, errors, filename)
	labelsNode := validateMappingField(metaNode, "labels", false, errors, filename)

	// Проверка поля metadata.name
	if nameNode != nil {
		if nameNode.Kind != yaml.ScalarNode {
			*errors = append(*errors, fmt.Sprintf("%s:%d name must be string", filename, nameNode.Line))
		} else if nameNode.Value == "" {
			*errors = append(*errors, fmt.Sprintf("%s:%d name has invalid format ''", filename, nameNode.Line))
		}
		// Примечание: при необходимости можно добавить проверку формата DNS-1123 для имени.
	}
	// Проверка поля metadata.namespace (если присутствует)
	if nsNode != nil {
		if nsNode.Kind != yaml.ScalarNode {
			*errors = append(*errors, fmt.Sprintf("%s:%d namespace must be string", filename, nsNode.Line))
		}
	}
	// Проверка поля metadata.labels (если присутствует)
	if labelsNode != nil {
		if labelsNode.Kind != yaml.MappingNode {
			*errors = append(*errors, fmt.Sprintf("%s:%d labels must be object", filename, labelsNode.Line))
		} else {
			// Все ключи и значения в labels должны быть строковыми литералами
			for i := 0; i < len(labelsNode.Content); i += 2 {
				k := labelsNode.Content[i]
				v := labelsNode.Content[i+1]
				if k.Kind != yaml.ScalarNode {
					*errors = append(*errors, fmt.Sprintf("%s:%d labels key must be string", filename, k.Line))
				}
				if v.Kind != yaml.ScalarNode {
					*errors = append(*errors, fmt.Sprintf("%s:%d labels value must be string", filename, v.Line))
				}
			}
		}
	}
}

// validateSpec проверяет узел spec (должен быть объектом) и его содержимое:
// os (необязательное, строка "linux"/"windows" или объект с полем name),
// containers (обязательное поле, массив объектов), и т.д.
func validateSpec(specNode *yaml.Node, errors *[]string, filename string) {
	if specNode == nil {
		return
	}
	if specNode.Kind != yaml.MappingNode {
		*errors = append(*errors, fmt.Sprintf("%s:%d spec must be object", filename, specNode.Line))
		return
	}
	// Поле spec.os (необязательное)
	osNode := getMappingValue(specNode, "os")
	if osNode != nil {
		var osName string
		if osNode.Kind == yaml.ScalarNode {
			// os задан как строка (например, "linux")
			osName = osNode.Value
		} else if osNode.Kind == yaml.MappingNode {
			// os задан как объект, извлекаем поле name
			nameNode := getMappingValue(osNode, "name")
			if nameNode == nil {
				*errors = append(*errors, fmt.Sprintf("%s:%d os.name is required", filename, osNode.Line))
			} else if nameNode.Kind != yaml.ScalarNode {
				*errors = append(*errors, fmt.Sprintf("%s:%d os.name must be string", filename, nameNode.Line))
			} else {
				osName = nameNode.Value
			}
		} else {
			*errors = append(*errors, fmt.Sprintf("%s:%d os must be string or object", filename, osNode.Line))
		}
		if osName != "" {
			if osName != "linux" && osName != "windows" {
				// Неподдерживаемое значение OS; определяем номер строки для сообщения об ошибке
				line := osNode.Line
				if osNode.Kind == yaml.MappingNode {
					if nameNode := getMappingValue(osNode, "name"); nameNode != nil {
						line = nameNode.Line
					}
				}
				*errors = append(*errors, fmt.Sprintf("%s:%d os has unsupported value '%s'", filename, line, osName))
			}
		}
	}
	// Поле spec.containers (обязательное)
	containersNode := getMappingValue(specNode, "containers")
	if containersNode == nil {
		*errors = append(*errors, fmt.Sprintf("%s: containers is required", filename))
	} else if containersNode.Kind != yaml.SequenceNode {
		*errors = append(*errors, fmt.Sprintf("%s:%d containers must be array", filename, containersNode.Line))
	} else {
		if len(containersNode.Content) == 0 {
			*errors = append(*errors, fmt.Sprintf("%s:%d containers must not be empty", filename, containersNode.Line))
		}
		// Проверяем каждый элемент массива containers
		for _, cNode := range containersNode.Content {
			validateContainer(cNode, errors, filename)
		}
	}
}

// validateContainer проверяет один объект контейнера в списке spec.containers.
// Проверяются поля: name, image, ports, readinessProbe, livenessProbe, resources.
func validateContainer(containerNode *yaml.Node, errors *[]string, filename string) {
	if containerNode.Kind != yaml.MappingNode {
		*errors = append(*errors, fmt.Sprintf("%s:%d container must be object", filename, containerNode.Line))
		return
	}
	// Извлекаем значения основных полей контейнера
	nameNode := validateMappingField(containerNode, "name", true, errors, filename)
	imageNode := validateMappingField(containerNode, "image", true, errors, filename)
	portsNode := getMappingValue(containerNode, "ports")              // необязательное поле
	readinessNode := getMappingValue(containerNode, "readinessProbe") // необязательное поле
	livenessNode := getMappingValue(containerNode, "livenessProbe")   // необязательное поле
	resourcesNode := validateMappingField(containerNode, "resources", true, errors, filename)

	// Проверка container.name
	if nameNode != nil {
		if nameNode.Kind != yaml.ScalarNode {
			*errors = append(*errors, fmt.Sprintf("%s:%d name must be string", filename, nameNode.Line))
		} else {
			// Имя контейнера должно соответствовать шаблону snake_case: только маленькие буквы и цифры, разделённые подчёркиванием
			matched, _ := regexp.MatchString("^[a-z0-9]+(_[a-z0-9]+)*$", nameNode.Value)
			if !matched {
				*errors = append(*errors, fmt.Sprintf("%s:%d name has invalid format '%s'", filename, nameNode.Line, nameNode.Value))
			}
		}
	}
	// Проверка container.image
	if imageNode != nil {
		if imageNode.Kind != yaml.ScalarNode {
			*errors = append(*errors, fmt.Sprintf("%s:%d image must be string", filename, imageNode.Line))
		} else {
			img := imageNode.Value
			// Формат image: должен начинаться с "registry.bigbrother.io/" и содержать ":" после этого префикса (указание тега образа)
			const prefix = "registry.bigbrother.io/"
			if !(len(img) > len(prefix) && strings.HasPrefix(img, prefix) && strings.Contains(img[len(prefix):], ":")) {
				*errors = append(*errors, fmt.Sprintf("%s:%d image has invalid format '%s'", filename, imageNode.Line, img))
			}
		}
	}
	// Проверка списка container.ports
	if portsNode != nil {
		validatePorts(portsNode, errors, filename)
	}
	// Проверка container.readinessProbe
	if readinessNode != nil {
		validateProbe(readinessNode, "readinessProbe", errors, filename)
	}
	// Проверка container.livenessProbe
	if livenessNode != nil {
		validateProbe(livenessNode, "livenessProbe", errors, filename)
	}
	// Проверка container.resources
	if resourcesNode != nil {
		if resourcesNode.Kind != yaml.MappingNode {
			*errors = append(*errors, fmt.Sprintf("%s:%d resources must be object", filename, resourcesNode.Line))
		} else {
			limitsNode := getMappingValue(resourcesNode, "limits")
			requestsNode := getMappingValue(resourcesNode, "requests")
			if limitsNode != nil {
				if limitsNode.Kind != yaml.MappingNode {
					*errors = append(*errors, fmt.Sprintf("%s:%d limits must be object", filename, limitsNode.Line))
				} else {
					checkResourceValues(limitsNode, errors, filename)
				}
			}
			if requestsNode != nil {
				if requestsNode.Kind != yaml.MappingNode {
					*errors = append(*errors, fmt.Sprintf("%s:%d requests must be object", filename, requestsNode.Line))
				} else {
					checkResourceValues(requestsNode, errors, filename)
				}
			}
		}
	}
}

// validatePorts проверяет список портов (spec.containers[].ports) —
// это должен быть массив объектов с полями containerPort (обязательное число) и protocol (необязательная строка "TCP"/"UDP").
func validatePorts(portsNode *yaml.Node, errors *[]string, filename string) {
	if portsNode.Kind != yaml.SequenceNode {
		*errors = append(*errors, fmt.Sprintf("%s:%d ports must be array", filename, portsNode.Line))
		return
	}
	for _, portEntry := range portsNode.Content {
		if portEntry.Kind != yaml.MappingNode {
			*errors = append(*errors, fmt.Sprintf("%s:%d ports entry must be object", filename, portEntry.Line))
			continue
		}
		cpNode := validateMappingField(portEntry, "containerPort", true, errors, filename)
		protoNode := getMappingValue(portEntry, "protocol")
		// Проверка поля containerPort
		if cpNode != nil {
			if cpNode.Kind != yaml.ScalarNode {
				*errors = append(*errors, fmt.Sprintf("%s:%d containerPort must be int", filename, cpNode.Line))
			} else {
				// Если YAML распарсил значение как строку (например, было в кавычках), его тег не будет "!!int"
				if cpNode.Tag != "!!int" {
					*errors = append(*errors, fmt.Sprintf("%s:%d containerPort must be int", filename, cpNode.Line))
				} else if val, err := strconv.Atoi(cpNode.Value); err == nil {
					// Проверка диапазона порта
					if val < 1 || val > 65535 {
						*errors = append(*errors, fmt.Sprintf("%s:%d containerPort value out of range", filename, cpNode.Line))
					}
				}
			}
		}
		// Проверка поля protocol (если указано)
		if protoNode != nil {
			if protoNode.Kind != yaml.ScalarNode {
				*errors = append(*errors, fmt.Sprintf("%s:%d protocol must be string", filename, protoNode.Line))
			} else {
				prot := protoNode.Value
				if prot != "TCP" && prot != "UDP" {
					*errors = append(*errors, fmt.Sprintf("%s:%d protocol has unsupported value '%s'", filename, protoNode.Line, prot))
				}
			}
		}
	}
}

// validateProbe проверяет узел readinessProbe/livenessProbe (объект) на наличие вложенного httpGet с полями path и port.
func validateProbe(probeNode *yaml.Node, fieldName string, errors *[]string, filename string) {
	if probeNode.Kind != yaml.MappingNode {
		*errors = append(*errors, fmt.Sprintf("%s:%d %s must be object", filename, probeNode.Line, fieldName))
		return
	}
	httpGetNode := getMappingValue(probeNode, "httpGet")
	if httpGetNode == nil {
		*errors = append(*errors, fmt.Sprintf("%s:%d httpGet is required", filename, probeNode.Line))
		return
	}
	if httpGetNode.Kind != yaml.MappingNode {
		*errors = append(*errors, fmt.Sprintf("%s:%d httpGet must be object", filename, httpGetNode.Line))
		return
	}
	pathNode := validateMappingField(httpGetNode, "path", true, errors, filename)
	portNode := validateMappingField(httpGetNode, "port", true, errors, filename)
	// Проверка httpGet.path
	if pathNode != nil {
		if pathNode.Kind != yaml.ScalarNode {
			*errors = append(*errors, fmt.Sprintf("%s:%d path must be string", filename, pathNode.Line))
		} else if !strings.HasPrefix(pathNode.Value, "/") {
			*errors = append(*errors, fmt.Sprintf("%s:%d path has invalid format '%s'", filename, pathNode.Line, pathNode.Value))
		}
	}
	// Проверка httpGet.port
	if portNode != nil {
		if portNode.Kind != yaml.ScalarNode {
			*errors = append(*errors, fmt.Sprintf("%s:%d port must be int", filename, portNode.Line))
		} else if portNode.Tag != "!!int" {
			*errors = append(*errors, fmt.Sprintf("%s:%d port must be int", filename, portNode.Line))
		} else if val, err := strconv.Atoi(portNode.Value); err == nil {
			if val < 1 || val > 65535 {
				*errors = append(*errors, fmt.Sprintf("%s:%d port value out of range", filename, portNode.Line))
			}
		}
	}
}

// checkResourceValues проверяет значения cpu и memory в секциях ресурсов (limits или requests).
// CPU должен быть неотрицательным целым числом, а memory — строкой с суффиксом единиц (Ki, Mi, Gi).
func checkResourceValues(resNode *yaml.Node, errors *[]string, filename string) {
	cpuNode := getMappingValue(resNode, "cpu")
	if cpuNode != nil {
		if cpuNode.Kind != yaml.ScalarNode {
			*errors = append(*errors, fmt.Sprintf("%s:%d cpu must be int", filename, cpuNode.Line))
		} else if cpuNode.Tag != "!!int" {
			*errors = append(*errors, fmt.Sprintf("%s:%d cpu must be int", filename, cpuNode.Line))
		} else if val, err := strconv.Atoi(cpuNode.Value); err == nil {
			if val < 0 {
				*errors = append(*errors, fmt.Sprintf("%s:%d cpu value out of range", filename, cpuNode.Line))
			}
		}
	}
	memNode := getMappingValue(resNode, "memory")
	if memNode != nil {
		if memNode.Kind != yaml.ScalarNode {
			*errors = append(*errors, fmt.Sprintf("%s:%d memory must be string", filename, memNode.Line))
		} else {
			memVal := memNode.Value
			matched, _ := regexp.MatchString(`^[0-9]+(Ki|Mi|Gi)$`, memVal)
			if !matched {
				*errors = append(*errors, fmt.Sprintf("%s:%d memory has invalid format '%s'", filename, memNode.Line, memVal))
			}
		}
	}
}

// validateDocument проверяет один YAML-документ (корневой узел).
// Требует, чтобы документ был объектом (мапой) и содержит обязательные поля apiVersion, kind, metadata, spec.
func validateDocument(doc *yaml.Node, filename string, errors *[]string) {
	if doc.Kind != yaml.MappingNode {
		*errors = append(*errors, fmt.Sprintf("%s:%d top-level document must be a mapping (object)", filename, doc.Line))
		return
	}
	// Проверка наличия обязательных полей верхнего уровня
	apiVersionNode := validateMappingField(doc, "apiVersion", true, errors, filename)
	kindNode := validateMappingField(doc, "kind", true, errors, filename)
	metaNode := validateMappingField(doc, "metadata", true, errors, filename)
	specNode := validateMappingField(doc, "spec", true, errors, filename)
	// Проверка содержимого каждого поля
	validateAPIVersion(apiVersionNode, errors, filename)
	validateKind(kindNode, errors, filename)
	validateMetadata(metaNode, errors, filename)
	validateSpec(specNode, errors, filename)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: yamlvalid <path-to-file>")
		os.Exit(1)
	}
	filename := os.Args[1]

	// Чтение содержимого YAML-файла
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", filename, err)
		os.Exit(1)
	}

	// Разбор YAML в узел yaml.Node
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", filename, err)
		os.Exit(1)
	}

	// Сбор ошибок валидации
	var errors []string
	if len(root.Content) == 0 {
		// Пустой файл или некорректный YAML
		errors = append(errors, fmt.Sprintf("%s: YAML content is empty or invalid", filename))
	} else {
		// Проверкa каждого документа (поддержка файлов с несколькими документами YAML)
		for _, doc := range root.Content {
			validateDocument(doc, filename, &errors)
		}
	}

	// Вывод ошибок, если они есть
	if len(errors) > 0 {
		for _, e := range errors {
			fmt.Fprintln(os.Stderr, e)
		}
		os.Exit(1)
	}
	// Если ошибок нет, возвращаем код 0 (успех)
	os.Exit(0)
}
