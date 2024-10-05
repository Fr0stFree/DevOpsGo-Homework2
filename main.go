package main

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	absPath string
	relPath string
)

type PodOS string

const (
	Linux   PodOS = "linux"
	Windows PodOS = "windows"
)

type Protocol string

const (
	TCP Protocol = "TCP"
	UDP Protocol = "UDP"
)

func init() {
	if len(os.Args[1:]) != 1 {
		panic("path to yaml is not provided")
	}
	filePath := os.Args[1]
	_, err := os.Stat(filePath)
	if errors.Is(err, os.ErrNotExist) {
		panic(fmt.Sprintf("%s does not exist", filePath))
	}
	absPath, _ = filepath.Abs(filePath)
	parentDir := filepath.Dir(filePath)
	relPath, _ = filepath.Rel(parentDir, filePath)
}

func main() {
	var root yaml.Node
	data, _ := os.ReadFile(absPath)
	err := yaml.Unmarshal(data, &root)

	if err != nil {
		panic(fmt.Errorf("cannot unmarshal file content: %w", err))
	}

	errs := validateManifesto(&root)

	for _, err := range errs {
		fmt.Println(err)
	}
}

func validateManifesto(root *yaml.Node) []error {
	errs := make([]error, 0)
	for _, doc := range root.Content {
		traverseCore(doc, &errs)
	}
	return errs
}

func checkRequiredFields(visited map[string]bool, required []string, errs *[]error) {
	for _, field := range required {
		if !visited[field] {
			*errs = append(*errs, NewRequiredFieldError(field))
		}
	}
}

func NewTypeError(key, mustBe string, line int) error {
	return fmt.Errorf("%s:%d %s must be %s", relPath, line, key, mustBe)
}

func NewRequiredFieldError(key string) error {
	return fmt.Errorf("%s is required", key)
}

func NewRequiredFieldErrorWithLine(key string, line int) error {
	return fmt.Errorf("%s:%d %s is required", relPath, line, key)
}

func NewOutOfRangeError(key string, line int) error {
	return fmt.Errorf("%s:%d %s value out of range", relPath, line, key)
}

func NewInvalidFormatError(key, value string, line int) error {
	return fmt.Errorf("%s:%d %s has invalid format '%s'", absPath, line, key, value)
}

func NewUnsupportedValueError(key, value string, line int) error {
	return fmt.Errorf("%s:%d %s has unsupported value '%s'", relPath, line, key, value)
}

func traverseCore(doc *yaml.Node, errs *[]error) {
	visited := make(map[string]bool)
	required := []string{"apiVersion", "kind", "metadata", "spec"}
	defer checkRequiredFields(visited, required, errs)

	for i := 0; i < len(doc.Content); i += 2 {
		key := doc.Content[i]
		value := doc.Content[i+1]

		switch key.Value {
		case "apiVersion":
			if value.Value != "v1" {
				*errs = append(*errs, NewUnsupportedValueError(key.Value, value.Value, key.Line))
			}
			visited["apiVersion"] = true
		case "kind":
			if value.Value != "Pod" {
				*errs = append(*errs, NewUnsupportedValueError(key.Value, value.Value, key.Line))
			}
			visited["kind"] = true
		case "metadata":
			traverseMetadata(value, errs)
			visited["metadata"] = true
		case "spec":
			traverseSpec(value, errs)
			visited["spec"] = true
		}
	}

}

func traverseMetadata(node *yaml.Node, errs *[]error) {
	visited := make(map[string]bool)
	required := []string{"name"}
	defer checkRequiredFields(visited, required, errs)

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]

		switch key.Value {
		case "name":
			if value.Value == "" {
				*errs = append(*errs, NewRequiredFieldErrorWithLine(key.Value, key.Line))
			}
			visited["name"] = true
		case "namespace":
			visited["namespace"] = true
		case "labels":
			traverseLabels(value, errs)
			visited["labels"] = true
		}
	}
}

func traverseLabels(node *yaml.Node, errs *[]error) {
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]

		if value.Kind != yaml.ScalarNode {
			*errs = append(*errs, NewTypeError(key.Value, "string", key.Line))
			continue
		}
	}
}

func traverseSpec(node *yaml.Node, errs *[]error) {
	visited := make(map[string]bool)
	required := []string{"containers"}
	defer checkRequiredFields(visited, required, errs)

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]

		switch key.Value {
		case "os":
			if PodOS(value.Value) != Linux && PodOS(value.Value) != Windows {
				*errs = append(*errs, NewUnsupportedValueError(key.Value, value.Value, key.Line))
			}
			visited["os"] = true
		case "containers":
			for _, container := range value.Content {
				traverseContainer(container, errs)
			}
			visited["containers"] = true
		}
	}
}

func traverseContainer(node *yaml.Node, errs *[]error) {
	visited := make(map[string]bool)
	required := []string{"name", "image", "resources"}
	defer checkRequiredFields(visited, required, errs)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]

		switch key.Value {
		case "name":
			if value.Value == "" {
				*errs = append(*errs, NewRequiredFieldErrorWithLine(key.Value, key.Line))
				visited["name"] = true
				continue
			}
			if value.Value != ToSnakeCase(value.Value) {
				*errs = append(*errs, NewInvalidFormatError(key.Value, value.Value, key.Line))
			}
			visited["name"] = true
		case "image":
			pattern := regexp.MustCompile(`^registry.bigbrother.io/(.*):(.*)$`)
			if !pattern.MatchString(value.Value) {
				*errs = append(*errs, NewInvalidFormatError(key.Value, value.Value, key.Line))
			}
			visited["image"] = true
		case "ports":
			for _, port := range value.Content {
				traverseContainerPort(port, errs)
			}
			visited["ports"] = true
		case "readinessProbe":
			traverseProbe(value, errs)
			visited["readinessProbe"] = true
		case "livenessProbe":
			traverseProbe(value, errs)
			visited["livenessProbe"] = true
		case "resources":
			traverseResources(value, errs)
			visited["resources"] = true
		}
	}
}

func traverseContainerPort(node *yaml.Node, errs *[]error) {
	required := []string{"containerPort"}
	visited := make(map[string]bool)
	defer checkRequiredFields(visited, required, errs)

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]

		switch key.Value {
		case "containerPort":
			if value.Tag != "!!int" {
				*errs = append(*errs, NewTypeError(key.Value, "int", key.Line))
				visited["containerPort"] = true
				continue
			}
			number, _ := strconv.Atoi(value.Value)
			if number < 0 || number > 65535 {
				*errs = append(*errs, NewOutOfRangeError(key.Value, key.Line))
			}
			visited["containerPort"] = true
		case "protocol":
			if Protocol(value.Value) != TCP && Protocol(value.Value) != UDP {
				*errs = append(*errs, NewUnsupportedValueError(key.Value, value.Value, key.Line))
			}
			visited["protocol"] = true
		}
	}
}

func traverseProbe(node *yaml.Node, errs *[]error) {
	visited := make(map[string]bool)
	required := []string{"httpGet"}
	defer checkRequiredFields(visited, required, errs)

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]

		switch key.Value {
		case "httpGet":
			traverseHTTPGet(value, errs)
			visited["httpGet"] = true
		}
	}
}

func traverseHTTPGet(node *yaml.Node, errs *[]error) {
	visited := make(map[string]bool)
	required := []string{"path", "port"}
	defer checkRequiredFields(visited, required, errs)

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]

		switch key.Value {
		case "path":
			if !strings.HasPrefix(value.Value, "/") {
				*errs = append(*errs, NewInvalidFormatError(key.Value, value.Value, key.Line))
			}
			visited["path"] = true
		case "port":
			if value.Tag != "!!int" {
				*errs = append(*errs, NewTypeError(key.Value, "int", key.Line))
				visited["port"] = true
				continue
			}
			number, _ := strconv.Atoi(value.Value)
			if number < 0 || number > 65535 {
				*errs = append(*errs, NewOutOfRangeError(key.Value, key.Line))
			}
			visited["port"] = true
		}
	}
}
func traverseResources(node *yaml.Node, errs *[]error) {
	required := []string{}
	visited := make(map[string]bool)
	defer checkRequiredFields(visited, required, errs)

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]

		switch key.Value {
		case "requests":
			traverseResourceDeclaration(value, errs)
			visited["requests"] = true
		case "limits":
			traverseResourceDeclaration(value, errs)
			visited["limits"] = true
		}
	}
}

func traverseResourceDeclaration(node *yaml.Node, errs *[]error) {
	required := []string{}
	visited := make(map[string]bool)
	defer checkRequiredFields(visited, required, errs)

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		value := node.Content[i+1]

		switch key.Value {
		case "cpu":
			if value.Tag != "!!int" {
				*errs = append(*errs, NewTypeError(key.Value, "int", key.Line))
				visited["cpu"] = true
				continue
			}
			number, _ := strconv.Atoi(value.Value)
			if number < 1 {
				*errs = append(*errs, NewOutOfRangeError(key.Value, key.Line))
			}
			visited["cpu"] = true
		case "memory":
			pattern := regexp.MustCompile(`^(\d+)(Mi|Gi|Ki)$`)
			result := pattern.FindStringSubmatch(value.Value)
			if len(result) != 3 {
				*errs = append(*errs, NewInvalidFormatError(key.Value, value.Value, key.Line))
				visited["memory"] = true
				continue
			}
			amount, err := strconv.Atoi(result[1])
			if err != nil {
				*errs = append(*errs, NewTypeError(key.Value, "int", key.Line))
				visited["memory"] = true
				continue
			}
			if amount < 1 {
				*errs = append(*errs, NewOutOfRangeError(key.Value, key.Line))
			}
			visited["memory"] = true
		}
	}
}

var matchFirstCap = regexp.MustCompile("(.)([A-Z][a-z]+)")
var matchAllCap = regexp.MustCompile("([a-z0-9])([A-Z])")

func ToSnakeCase(str string) string {
	snake := matchFirstCap.ReplaceAllString(str, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}
