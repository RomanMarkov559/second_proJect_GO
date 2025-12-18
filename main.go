package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Validator struct {
	errors []string
	file   string

	// Скомпилированные регулярные выражения
	nameRegex   *regexp.Regexp
	memoryRegex *regexp.Regexp
}

func NewValidator(file string) *Validator {
	// Компилируем регулярные выражения один раз
	nameRegex := regexp.MustCompile(`^[a-z][a-z0-9_]*(_[a-z0-9]+)*$`)
	memoryRegex := regexp.MustCompile(`^\d+(Gi|Mi|Ki)$`)

	return &Validator{
		errors:      []string{},
		file:        file,
		nameRegex:   nameRegex,
		memoryRegex: memoryRegex,
	}
}

func (v *Validator) addError(line int, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if line > 0 {
		v.errors = append(v.errors, fmt.Sprintf("%s:%d %s", v.file, line, msg))
	} else {
		v.errors = append(v.errors, fmt.Sprintf("%s %s", v.file, msg))
	}
}

func (v *Validator) PrintErrors() {
	for _, err := range v.errors {
		fmt.Fprintln(os.Stderr, err)
	}
}

func (v *Validator) Validate(node *yaml.Node) bool {
	// Проверяем что это документ с картой
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		v.addError(node.Line, "invalid YAML structure")
		return false
	}

	root := node.Content[0]
	v.validateTopLevel(root)

	return len(v.errors) == 0
}

func (v *Validator) validateTopLevel(node *yaml.Node) {
	// Проверяем обязательные поля верхнего уровня
	apiVersionNode := v.getField(node, "apiVersion")
	if apiVersionNode == nil {
		v.addError(node.Line, "apiVersion is required")
	} else if apiVersionNode.Value != "v1" {
		v.addError(apiVersionNode.Line, "apiVersion has unsupported value '%s'", apiVersionNode.Value)
	}

	kindNode := v.getField(node, "kind")
	if kindNode == nil {
		v.addError(node.Line, "kind is required")
	} else if kindNode.Value != "Pod" {
		v.addError(kindNode.Line, "kind has unsupported value '%s'", kindNode.Value)
	}

	metadataNode := v.getField(node, "metadata")
	if metadataNode == nil {
		v.addError(node.Line, "metadata is required")
	} else {
		v.validateMetadata(metadataNode)
	}

	specNode := v.getField(node, "spec")
	if specNode == nil {
		v.addError(node.Line, "spec is required")
	} else {
		v.validateSpec(specNode)
	}
}

func (v *Validator) validateMetadata(node *yaml.Node) {
	nameNode := v.getField(node, "name")
	if nameNode == nil {
		v.addError(node.Line, "metadata.name is required")
	} else if nameNode.Value == "" {
		v.addError(nameNode.Line, "metadata.name is required")
	}
}

func (v *Validator) validateSpec(node *yaml.Node) {
	// Проверяем os если есть
	osNode := v.getField(node, "os")
	if osNode != nil {
		if osNode.Kind == yaml.MappingNode {
			v.validateOS(osNode)
		} else if osNode.Kind == yaml.ScalarNode {
			// os как скаляр (например: os: linux)
			if osNode.Value != "linux" && osNode.Value != "windows" {
				v.addError(osNode.Line, "os has unsupported value '%s'", osNode.Value)
			}
		}
	}

	// Проверяем обязательные containers
	containersNode := v.getField(node, "containers")
	if containersNode == nil {
		v.addError(node.Line, "spec.containers is required")
	} else {
		v.validateContainers(containersNode)
	}
}

func (v *Validator) validateOS(node *yaml.Node) {
	nameNode := v.getField(node, "name")
	if nameNode == nil {
		v.addError(node.Line, "os.name is required")
	} else if nameNode.Value != "linux" && nameNode.Value != "windows" {
		v.addError(nameNode.Line, "os has unsupported value '%s'", nameNode.Value)
	}
}

func (v *Validator) validateContainers(node *yaml.Node) {
	if node.Kind != yaml.SequenceNode {
		v.addError(node.Line, "spec.containers must be an array")
		return
	}

	for i, containerNode := range node.Content {
		v.validateContainer(containerNode, i)
	}
}

func (v *Validator) validateContainer(node *yaml.Node, index int) {
	// Проверка имени
	nameNode := v.getField(node, "name")
	if nameNode == nil {
		v.addError(node.Line, "spec.containers[%d].name is required", index)
	} else if nameNode.Value == "" {
		v.addError(nameNode.Line, "spec.containers[%d].name is required", index)
	} else if !v.nameRegex.MatchString(nameNode.Value) {
		v.addError(nameNode.Line, "spec.containers[%d].name has invalid format '%s'", index, nameNode.Value)
	}

	// Проверка image
	imageNode := v.getField(node, "image")
	if imageNode == nil {
		v.addError(node.Line, "spec.containers[%d].image is required", index)
	} else if imageNode.Value == "" {
		v.addError(imageNode.Line, "spec.containers[%d].image is required", index)
	} else {
		if !strings.HasPrefix(imageNode.Value, "registry.bigbrother.io/") {
			v.addError(imageNode.Line, "spec.containers[%d].image has invalid format '%s'", index, imageNode.Value)
		} else if !strings.Contains(imageNode.Value, ":") {
			v.addError(imageNode.Line, "spec.containers[%d].image has invalid format '%s'", index, imageNode.Value)
		}
	}

	// Проверка ports если есть
	portsNode := v.getField(node, "ports")
	if portsNode != nil && portsNode.Kind == yaml.SequenceNode {
		for j, portNode := range portsNode.Content {
			v.validateContainerPort(portNode, index, j)
		}
	}

	// Проверка readinessProbe если есть
	readinessProbeNode := v.getField(node, "readinessProbe")
	if readinessProbeNode != nil {
		v.validateProbe(readinessProbeNode, index, "readinessProbe")
	}

	// Проверка livenessProbe если есть
	livenessProbeNode := v.getField(node, "livenessProbe")
	if livenessProbeNode != nil {
		v.validateProbe(livenessProbeNode, index, "livenessProbe")
	}

	// Проверка resources (обязательно)
	resourcesNode := v.getField(node, "resources")
	if resourcesNode == nil {
		v.addError(node.Line, "spec.containers[%d].resources is required", index)
	} else {
		v.validateResources(resourcesNode, index)
	}
}

func (v *Validator) validateContainerPort(node *yaml.Node, containerIndex, portIndex int) {
	// Проверка containerPort
	portNode := v.getField(node, "containerPort")
	if portNode == nil {
		v.addError(node.Line, "spec.containers[%d].ports[%d].containerPort is required", containerIndex, portIndex)
	} else {
		port, err := strconv.Atoi(portNode.Value)
		if err != nil {
			v.addError(portNode.Line, "spec.containers[%d].ports[%d].containerPort must be int", containerIndex, portIndex)
		} else if port <= 0 || port >= 65536 {
			v.addError(portNode.Line, "spec.containers[%d].ports[%d].containerPort value out of range", containerIndex, portIndex)
		}
	}

	// Проверка protocol если есть
	protocolNode := v.getField(node, "protocol")
	if protocolNode != nil {
		if protocolNode.Value != "TCP" && protocolNode.Value != "UDP" {
			v.addError(protocolNode.Line, "spec.containers[%d].ports[%d].protocol has unsupported value '%s'", containerIndex, portIndex, protocolNode.Value)
		}
	}
}

func (v *Validator) validateProbe(node *yaml.Node, containerIndex int, probeType string) {
	httpGetNode := v.getField(node, "httpGet")
	if httpGetNode == nil {
		v.addError(node.Line, "spec.containers[%d].%s.httpGet is required", containerIndex, probeType)
	} else {
		v.validateHTTPGetAction(httpGetNode, containerIndex, probeType)
	}
}

func (v *Validator) validateHTTPGetAction(node *yaml.Node, containerIndex int, probeType string) {
	pathNode := v.getField(node, "path")
	if pathNode == nil {
		v.addError(node.Line, "spec.containers[%d].%s.httpGet.path is required", containerIndex, probeType)
	} else if !strings.HasPrefix(pathNode.Value, "/") {
		v.addError(pathNode.Line, "spec.containers[%d].%s.httpGet.path has invalid format '%s'", containerIndex, probeType, pathNode.Value)
	}

	portNode := v.getField(node, "port")
	if portNode == nil {
		v.addError(node.Line, "spec.containers[%d].%s.httpGet.port is required", containerIndex, probeType)
	} else {
		port, err := strconv.Atoi(portNode.Value)
		if err != nil {
			v.addError(portNode.Line, "spec.containers[%d].%s.httpGet.port must be int", containerIndex, probeType)
		} else if port <= 0 || port >= 65536 {
			// ИСПРАВЛЕНИЕ: простой формат "port value out of range"
			v.addError(portNode.Line, "port value out of range")
		}
	}
}

func (v *Validator) validateResources(node *yaml.Node, containerIndex int) {
	// Проверяем limits если есть
	limitsNode := v.getField(node, "limits")
	if limitsNode != nil {
		v.validateResourceMap(limitsNode, containerIndex, "limits")
	}

	// Проверяем requests если есть
	requestsNode := v.getField(node, "requests")
	if requestsNode != nil {
		v.validateResourceMap(requestsNode, containerIndex, "requests")
	}
}

func (v *Validator) validateResourceMap(node *yaml.Node, containerIndex int, mapType string) {
	if node.Kind != yaml.MappingNode {
		v.addError(node.Line, "spec.containers[%d].resources.%s must be an object", containerIndex, mapType)
		return
	}

	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]

		switch keyNode.Value {
		case "cpu":
			if valueNode.Kind != yaml.ScalarNode {
				v.addError(valueNode.Line, "spec.containers[%d].resources.%s.cpu must be int", containerIndex, mapType)
			} else {
				// ИСПРАВЛЕНИЕ: проверяем что значение состоит ТОЛЬКО из цифр
				// "1" проходит, "1.5" нет, " 1 " нет
				hasNonDigit := false
				for _, ch := range valueNode.Value {
					if ch < '0' || ch > '9' {
						hasNonDigit = true
						break
					}
				}
				if hasNonDigit {
					v.addError(valueNode.Line, "spec.containers[%d].resources.%s.cpu must be int", containerIndex, mapType)
				} else {
					// Проверяем диапазон
					cpu, _ := strconv.Atoi(valueNode.Value)
					if cpu <= 0 {
						v.addError(valueNode.Line, "spec.containers[%d].resources.%s.cpu value out of range", containerIndex, mapType)
					}
				}
			}
		case "memory":
			if valueNode.Kind != yaml.ScalarNode {
				v.addError(valueNode.Line, "spec.containers[%d].resources.%s.memory must be string", containerIndex, mapType)
			} else if !v.memoryRegex.MatchString(valueNode.Value) {
				v.addError(valueNode.Line, "spec.containers[%d].resources.%s.memory has invalid format '%s'", containerIndex, mapType, valueNode.Value)
			}
		default:
			v.addError(keyNode.Line, "spec.containers[%d].resources.%s.%s has unsupported resource type", containerIndex, mapType, keyNode.Value)
		}
	}
}

func (v *Validator) getField(node *yaml.Node, fieldName string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == fieldName {
			return node.Content[i+1]
		}
	}
	return nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <yaml-file>\n", os.Args[0])
		os.Exit(1)
	}

	fileName := os.Args[1]

	// Чтение файла
	content, err := os.ReadFile(fileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot read file: %v\n", fileName, err)
		os.Exit(1)
	}

	// Парсинг YAML
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot unmarshal YAML: %v\n", fileName, err)
		os.Exit(1)
	}

	// Валидация
	validator := NewValidator(fileName)

	if !validator.Validate(&root) {
		validator.PrintErrors()
		os.Exit(1)
	}

	// Успешная валидация
	os.Exit(0)
}