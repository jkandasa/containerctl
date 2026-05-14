package config

import (
	"fmt"
	"os"
	"strings"
)

// UpdateContainerImage rewrites the image field for the named container in the
// stack file, preserving all surrounding formatting and comments.
func UpdateContainerImage(file, containerName, newImage string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	inTarget := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if rest, ok := strings.CutPrefix(trimmed, "- name:"); ok {
			inTarget = strings.TrimSpace(rest) == containerName
			continue
		}

		if inTarget && strings.HasPrefix(trimmed, "image:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "image: " + newImage
			return os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0o644)
		}
	}

	return fmt.Errorf("container %q not found in %s", containerName, file)
}
