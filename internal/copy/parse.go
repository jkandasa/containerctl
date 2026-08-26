// Package copy implements host ↔ container file copy (docker-cp semantics).
package copy

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Spec is one side of a copy: either a host path or NAME:PATH in a container.
type Spec struct {
	Container string // logical stack name; empty means host
	Path      string
}

// Host reports whether the spec refers to the host (including "-" for stdin/stdout).
func (s Spec) Host() bool { return s.Container == "" }

// Stdio reports whether the spec is the "-" stdin/stdout stream.
func (s Spec) Stdio() bool { return s.Host() && s.Path == "-" }

// Direction is which way a copy runs.
type Direction int

const (
	DirFromContainer Direction = iota + 1
	DirToContainer
	DirAcrossContainers
)

// ParseArg splits a copy argument into a container name and path.
//
//	NAME:PATH  → container NAME, path PATH
//	/abs/path  → host
//	./rel:x    → host (leading "." wins over the colon)
//	-          → host stdin/stdout
func ParseArg(arg string) Spec {
	if arg == "-" {
		return Spec{Path: "-"}
	}
	if filepath.IsAbs(arg) || filepath.VolumeName(arg) != "" {
		return Spec{Path: arg}
	}
	parts := strings.SplitN(arg, ":", 2)
	if len(parts) == 1 || parts[0] == "" || strings.HasPrefix(parts[0], ".") {
		return Spec{Path: arg}
	}
	// A slash in the left-hand side is a host path, not a container name.
	if strings.ContainsAny(parts[0], `/\`) {
		return Spec{Path: arg}
	}
	return Spec{Container: parts[0], Path: parts[1]}
}

// Resolve decides copy direction from the two arguments.
func Resolve(src, dest string) (Spec, Spec, Direction, error) {
	if src == "" {
		return Spec{}, Spec{}, 0, fmt.Errorf("source can not be empty")
	}
	if dest == "" {
		return Spec{}, Spec{}, 0, fmt.Errorf("destination can not be empty")
	}
	srcSpec := ParseArg(src)
	destSpec := ParseArg(dest)

	switch {
	case !srcSpec.Host() && !destSpec.Host():
		if srcSpec.Path == "" || destSpec.Path == "" {
			return Spec{}, Spec{}, 0, fmt.Errorf("container path must not be empty; use NAME:PATH")
		}
		return srcSpec, destSpec, DirAcrossContainers, nil
	case srcSpec.Host() && destSpec.Host():
		return Spec{}, Spec{}, 0, fmt.Errorf("must specify at least one container path (NAME:PATH)")
	case !srcSpec.Host():
		if srcSpec.Path == "" {
			return Spec{}, Spec{}, 0, fmt.Errorf("container path must not be empty; use NAME:PATH")
		}
		return srcSpec, destSpec, DirFromContainer, nil
	default:
		if destSpec.Path == "" {
			return Spec{}, Spec{}, 0, fmt.Errorf("container path must not be empty; use NAME:PATH")
		}
		return srcSpec, destSpec, DirToContainer, nil
	}
}
