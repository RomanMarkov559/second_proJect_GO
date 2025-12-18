package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Printf("Usage: %s <yaml-file>\n", os.Args[0])
		os.Exit(1)
	}

	filename := os.Args[1]
	
	// Чтение файла
	content, err := os.ReadFile(filename)
	if err != nil {
		fmt.Printf("cannot read file: %v\n", err)
		os.Exit(1)
	}

	// Парсинг YAML
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Printf("cannot unmarshal file content: %v\n", err)
		os.Exit(1)
	}

	// Валидация
	errors := validateYAML(filename, &root)
	
	if len(errors) > 0 {
		for _, err := range errors {
			fmt.Println(err)
		}
		os.Exit(1)
	}
}

func validateYAML(filename string, root *yaml.Node) []string {
	errors := []string{}
	
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		errors = append(errors, "invalid YAML document")
		return errors
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		errors = append(errors, fmt.Sprintf("%s:%d root must be a mapping", getBaseFilename(filename), doc.Line))
		return errors
	}

	// Проверяем верхний уровень
	errors = validateTopLevel(filename, doc, errors)
	
	return errors
}

func validateTopLevel(filename string, node *yaml.Node, errors []string) []string {
	// Получаем все поля верхнего уровня
	fields := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			key := node.Content[i]
			value := node.Content[i+1]
			fields[key.Value] = value
		}
	}

	// 1. Проверка apiVersion
	if apiVersion, ok := fields["apiVersion"]; !ok {
		errors = append(errors, "apiVersion is required")
	} else if apiVersion.Value != "v1" {
		errors = append(errors, fmt.Sprintf("%s:%d apiVersion has unsupported value '%s'", 
			getBaseFilename(filename), apiVersion.Line, apiVersion.Value))
	}

	// 2. Проверка kind
	if kind, ok := fields["kind"]; !ok {
		errors = append(errors, "kind is required")
	} else if kind.Value != "Pod" {
		errors = append(errors, fmt.Sprintf("%s:%d kind has unsupported value '%s'", 
			getBaseFilename(filename), kind.Line, kind.Value))
	}

	// 3. Проверка metadata
	if metadata, ok := fields["metadata"]; !ok {
		errors = append(errors, "metadata is required")
	} else {
		errors = validateMetadata(filename, metadata, errors)
	}

	// 4. Проверка spec
	if spec, ok := fields["spec"]; !ok {
		errors = append(errors, "spec is required")
	} else {
		errors = validateSpec(filename, spec, errors)
	}

	return errors
}

func validateMetadata(filename string, node *yaml.Node, errors []string) []string {
	if node.Kind != yaml.MappingNode {
		errors = append(errors, fmt.Sprintf("%s:%d metadata must be a mapping", getBaseFilename(filename), node.Line))
		return errors
	}

	// Ищем поле name
	fields := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			key := node.Content[i]
			value := node.Content[i+1]
			fields[key.Value] = value
		}
	}

	if name, ok := fields["name"]; !ok || name.Value == "" {
		line := node.Line
		if name != nil {
			line = name.Line
		}
		errors = append(errors, fmt.Sprintf("%s:%d name is required", getBaseFilename(filename), line))
	}

	return errors
}

func validateSpec(filename string, node *yaml.Node, errors []string) []string {
	if node.Kind != yaml.MappingNode {
		errors = append(errors, fmt.Sprintf("%s:%d spec must be a mapping", getBaseFilename(filename), node.Line))
		return errors
	}

	// Получаем поля spec
	fields := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			key := node.Content[i]
			value := node.Content[i+1]
			fields[key.Value] = value
		}
	}

	// Проверка os если есть
	if osNode, ok := fields["os"]; ok {
		if osNode.Value != "linux" && osNode.Value != "windows" {
			errors = append(errors, fmt.Sprintf("%s:%d os has unsupported value '%s'", 
				getBaseFilename(filename), osNode.Line, osNode.Value))
		}
	}

	// Проверка containers
	containers, ok := fields["containers"]
	if !ok {
		line := getKeyLine(node, "containers")
		errors = append(errors, fmt.Sprintf("%s:%d containers is required", getBaseFilename(filename), line))
		return errors
	}

	if containers.Kind != yaml.SequenceNode {
		errors = append(errors, fmt.Sprintf("%s:%d containers must be a list", getBaseFilename(filename), containers.Line))
		return errors
	}

	if len(containers.Content) == 0 {
		errors = append(errors, fmt.Sprintf("%s:%d containers is required", getBaseFilename(filename), containers.Line))
		return errors
	}

	// Валидируем каждый контейнер
	containerNames := make(map[string]bool)
	for i, container := range containers.Content {
		errors = validateContainer(filename, container, i, containerNames, errors)
	}

	return errors
}

func validateContainer(filename string, node *yaml.Node, index int, containerNames map[string]bool, errors []string) []string {
	if node.Kind != yaml.MappingNode {
		errors = append(errors, fmt.Sprintf("%s:%d container must be a mapping", getBaseFilename(filename), node.Line))
		return errors
	}

	// Получаем поля контейнера
	fields := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			key := node.Content[i]
			value := node.Content[i+1]
			fields[key.Value] = value
		}
	}

	// Проверка name
	name, ok := fields["name"]
	if !ok || name.Value == "" {
		line := getKeyLine(node, "name")
		errors = append(errors, fmt.Sprintf("%s:%d name is required", getBaseFilename(filename), line))
	} else {
		// Проверка формата snake_case
		matched, _ := regexp.MatchString(`^[a-z]+(_[a-z]+)*$`, name.Value)
		if !matched {
			errors = append(errors, fmt.Sprintf("%s:%d name has invalid format '%s'", 
				getBaseFilename(filename), name.Line, name.Value))
		}
		
		// Проверка уникальности
		if containerNames[name.Value] {
			errors = append(errors, fmt.Sprintf("%s:%d container name '%s' must be unique", 
				getBaseFilename(filename), name.Line, name.Value))
		}
		containerNames[name.Value] = true
	}

	// Проверка image
	image, ok := fields["image"]
	if !ok || image.Value == "" {
		line := getKeyLine(node, "image")
		errors = append(errors, fmt.Sprintf("%s:%d image is required", getBaseFilename(filename), line))
	} else {
		// Проверка формата
		if !strings.HasPrefix(image.Value, "registry.bigbrother.io/") {
			errors = append(errors, fmt.Sprintf("%s:%d image has invalid format '%s'", 
				getBaseFilename(filename), image.Line, image.Value))
		} else {
			// Проверка наличия тега
			parts := strings.Split(image.Value, ":")
			if len(parts) != 2 || parts[1] == "" {
				errors = append(errors, fmt.Sprintf("%s:%d image has invalid format '%s'", 
					getBaseFilename(filename), image.Line, image.Value))
			}
		}
	}

	// Проверка ports если есть
	if ports, ok := fields["ports"]; ok {
		if ports.Kind == yaml.SequenceNode {
			for i, port := range ports.Content {
				errors = validatePort(filename, port, i, errors)
			}
		}
	}

	// Проверка readinessProbe если есть
	if probe, ok := fields["readinessProbe"]; ok {
		errors = validateProbe(filename, probe, "readinessProbe", errors)
	}

	// Проверка livenessProbe если есть
	if probe, ok := fields["livenessProbe"]; ok {
		errors = validateProbe(filename, probe, "livenessProbe", errors)
	}

	// Проверка resources (обязательное поле)
	resources, ok := fields["resources"]
	if !ok {
		line := getKeyLine(node, "resources")
		errors = append(errors, fmt.Sprintf("%s:%d resources is required", getBaseFilename(filename), line))
	} else {
		errors = validateResources(filename, resources, errors)
	}

	return errors
}

func validatePort(filename string, node *yaml.Node, index int, errors []string) []string {
	if node.Kind != yaml.MappingNode {
		errors = append(errors, fmt.Sprintf("%s:%d port must be a mapping", getBaseFilename(filename), node.Line))
		return errors
	}

	// Получаем поля порта
	fields := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			key := node.Content[i]
			value := node.Content[i+1]
			fields[key.Value] = value
		}
	}

	// Проверка containerPort
	port, ok := fields["containerPort"]
	if !ok {
		line := getKeyLine(node, "containerPort")
		errors = append(errors, fmt.Sprintf("%s:%d containerPort is required", getBaseFilename(filename), line))
	} else {
		// Проверяем что это число
		if port.Kind != yaml.ScalarNode {
			errors = append(errors, fmt.Sprintf("%s:%d containerPort must be int", getBaseFilename(filename), port.Line))
		} else {
			portValue, err := strconv.Atoi(port.Value)
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s:%d containerPort must be int", getBaseFilename(filename), port.Line))
			} else if portValue <= 0 || portValue >= 65536 {
				errors = append(errors, fmt.Sprintf("%s:%d containerPort value out of range", getBaseFilename(filename), port.Line))
			}
		}
	}

	// Проверка protocol если есть
	if protocol, ok := fields["protocol"]; ok {
		if protocol.Value != "TCP" && protocol.Value != "UDP" {
			errors = append(errors, fmt.Sprintf("%s:%d protocol has unsupported value '%s'", 
				getBaseFilename(filename), protocol.Line, protocol.Value))
		}
	}

	return errors
}

func validateProbe(filename string, node *yaml.Node, probeType string, errors []string) []string {
	if node.Kind != yaml.MappingNode {
		errors = append(errors, fmt.Sprintf("%s:%d %s must be a mapping", getBaseFilename(filename), node.Line, probeType))
		return errors
	}

	// Получаем поля probe
	fields := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			key := node.Content[i]
			value := node.Content[i+1]
			fields[key.Value] = value
		}
	}

	// Проверка httpGet
	httpGet, ok := fields["httpGet"]
	if !ok {
		line := getKeyLine(node, "httpGet")
		errors = append(errors, fmt.Sprintf("%s:%d httpGet is required", getBaseFilename(filename), line))
		return errors
	}

	errors = validateHTTPGet(filename, httpGet, errors)

	return errors
}

func validateHTTPGet(filename string, node *yaml.Node, errors []string) []string {
	if node.Kind != yaml.MappingNode {
		errors = append(errors, fmt.Sprintf("%s:%d httpGet must be a mapping", getBaseFilename(filename), node.Line))
		return errors
	}

	// Получаем поля httpGet
	fields := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			key := node.Content[i]
			value := node.Content[i+1]
			fields[key.Value] = value
		}
	}

	// Проверка path
	path, ok := fields["path"]
	if !ok || path.Value == "" {
		line := getKeyLine(node, "path")
		errors = append(errors, fmt.Sprintf("%s:%d path is required", getBaseFilename(filename), line))
	} else if !strings.HasPrefix(path.Value, "/") {
		errors = append(errors, fmt.Sprintf("%s:%d path must be absolute", getBaseFilename(filename), path.Line))
	}

	// Проверка port
	port, ok := fields["port"]
	if !ok {
		line := getKeyLine(node, "port")
		errors = append(errors, fmt.Sprintf("%s:%d port is required", getBaseFilename(filename), line))
	} else {
		// Проверяем что это число
		if port.Kind != yaml.ScalarNode {
			errors = append(errors, fmt.Sprintf("%s:%d port must be int", getBaseFilename(filename), port.Line))
		} else {
			portValue, err := strconv.Atoi(port.Value)
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s:%d port must be int", getBaseFilename(filename), port.Line))
			} else if portValue <= 0 || portValue >= 65536 {
				errors = append(errors, fmt.Sprintf("%s:%d port value out of range", getBaseFilename(filename), port.Line))
			}
		}
	}

	return errors
}

func validateResources(filename string, node *yaml.Node, errors []string) []string {
	if node.Kind != yaml.MappingNode {
		errors = append(errors, fmt.Sprintf("%s:%d resources must be a mapping", getBaseFilename(filename), node.Line))
		return errors
	}

	// Получаем поля resources
	fields := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			key := node.Content[i]
			value := node.Content[i+1]
			fields[key.Value] = value
		}
	}

	// Проверяем limits если есть
	if limits, ok := fields["limits"]; ok {
		errors = validateResourceList(filename, limits, "limits", errors)
	}

	// Проверяем requests если есть
	if requests, ok := fields["requests"]; ok {
		errors = validateResourceList(filename, requests, "requests", errors)
	}

	return errors
}

func validateResourceList(filename string, node *yaml.Node, resourceType string, errors []string) []string {
	if node.Kind != yaml.MappingNode {
		errors = append(errors, fmt.Sprintf("%s:%d %s must be a mapping", getBaseFilename(filename), node.Line, resourceType))
		return errors
	}

	// Получаем поля ресурсов
	fields := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		if i+1 < len(node.Content) {
			key := node.Content[i]
			value := node.Content[i+1]
			fields[key.Value] = value
		}
	}

	// Проверка cpu
	if cpu, ok := fields["cpu"]; ok {
		_, err := strconv.Atoi(cpu.Value)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s:%d cpu must be int", getBaseFilename(filename), cpu.Line))
		}
	}

	// Проверка memory
	if memory, ok := fields["memory"]; ok {
		matched, _ := regexp.MatchString(`^[0-9]+(Gi|Mi|Ki)$`, memory.Value)
		if !matched {
			errors = append(errors, fmt.Sprintf("%s:%d memory has invalid format '%s'", 
				getBaseFilename(filename), memory.Line, memory.Value))
		}
	}

	return errors
}

func getKeyLine(node *yaml.Node, key string) int {
	if node.Kind != yaml.MappingNode {
		return node.Line
	}

	for i := 0; i < len(node.Content); i += 2 {
		if i < len(node.Content) && node.Content[i].Value == key {
			return node.Content[i].Line
		}
	}
	return node.Line
}

// Получаем только имя файла без пути
func getBaseFilename(filename string) string {
	return filepath.Base(filename)
}