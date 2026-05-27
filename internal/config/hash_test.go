package config

import (
	"testing"
)

func TestHash_Stability(t *testing.T) {
	c := &Container{
		Name:    "test",
		Image:   "nginx:1.27",
		Restart: "unless-stopped",
		Env: map[string]string{
			"B": "two",
			"A": "one",
			"Z": "last",
		},
		Labels: map[string]string{
			"com.example.z": "999",
			"com.example.a": "111",
		},
		Resources: Resources{
			CPUs:   "1.5",
			Memory: "512m",
		},
		Networks: []string{"frontend", "backend"},
	}

	h1 := Hash(c)
	// Call many times; must be identical every time (including across map iteration randomness)
	for i := 0; i < 100; i++ {
		if got := Hash(c); got != h1 {
			t.Fatalf("Hash is not stable: call %d produced %s, want %s", i, got, h1)
		}
	}
}

func TestHash_EnvOrderDoesNotMatter(t *testing.T) {
	c1 := &Container{
		Image: "app:1",
		Env: map[string]string{
			"FOO": "bar",
			"BAZ": "quux",
		},
	}
	c2 := &Container{
		Image: "app:1",
		Env: map[string]string{
			"BAZ": "quux",
			"FOO": "bar",
		},
	}

	if Hash(c1) != Hash(c2) {
		t.Error("Hash must be identical regardless of map key order in Env")
	}
}

func TestHash_LabelsOrderDoesNotMatter(t *testing.T) {
	c1 := &Container{
		Image: "app:1",
		Labels: map[string]string{
			"foo": "1",
			"bar": "2",
		},
	}
	c2 := &Container{
		Image: "app:1",
		Labels: map[string]string{
			"bar": "2",
			"foo": "1",
		},
	}

	if Hash(c1) != Hash(c2) {
		t.Error("Hash must be identical regardless of map key order in Labels")
	}
}

func TestHash_DifferentEnvProducesDifferentHash(t *testing.T) {
	c1 := &Container{Image: "app:1", Env: map[string]string{"A": "1"}}
	c2 := &Container{Image: "app:1", Env: map[string]string{"A": "2"}}

	if Hash(c1) == Hash(c2) {
		t.Error("Different env values must produce different hashes")
	}
}

func TestHash_ContainerctlLabelsAreExcluded(t *testing.T) {
	c1 := &Container{
		Image: "app:1",
		Labels: map[string]string{
			"my.label": "value",
		},
	}
	c2 := &Container{
		Image: "app:1",
		Labels: map[string]string{
			"my.label":           "value",
			"containerctl.foo":   "should-be-ignored",
			"containerctl.project": "also-ignored",
		},
	}

	if Hash(c1) != Hash(c2) {
		t.Error("containerctl.* labels must be excluded from the hash")
	}
}

func TestHash_NilVsEmptyMapsAreEquivalent(t *testing.T) {
	c1 := &Container{Image: "app:1", Env: nil, Labels: nil}
	c2 := &Container{Image: "app:1", Env: map[string]string{}, Labels: map[string]string{}}

	h1 := Hash(c1)
	h2 := Hash(c2)
	if h1 != h2 {
		t.Errorf("nil and empty maps must produce the same hash: %s != %s", h1, h2)
	}
}

func TestHash_SortedSlicesAreStable(t *testing.T) {
	c := &Container{
		Image:   "app:1",
		DNS:     []string{"8.8.8.8", "1.1.1.1"},
		CapAdd:  []string{"NET_ADMIN", "SYS_ADMIN"},
		CapDrop: []string{"ALL"},
	}

	h1 := Hash(c)
	for i := 0; i < 50; i++ {
		if got := Hash(c); got != h1 {
			t.Fatalf("Hash changed on iteration %d for sorted-slice container", i)
		}
	}
}
