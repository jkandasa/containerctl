package reconcile

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
	"github.com/jkandasa/containerctl/internal/registry"
)

// fakeRuntime is a minimal implementation of rt.Runtime for testing the planner.
// Only ListNetworks, ListContainers, and InspectContainer are implemented.
type fakeRuntime struct {
	networks []rt.NetworkInfo
	containers []rt.ContainerInfo
	// inspect is used to simulate unmanaged containers for conflict detection
	inspect map[string]*rt.ContainerInfo
}

func (f *fakeRuntime) ListNetworks(ctx context.Context, filters rt.Filters) ([]rt.NetworkInfo, error) {
	return f.networks, nil
}

func (f *fakeRuntime) ListContainers(ctx context.Context, filters rt.Filters) ([]rt.ContainerInfo, error) {
	return f.containers, nil
}

func (f *fakeRuntime) InspectContainer(ctx context.Context, nameOrID string) (*rt.ContainerInfo, error) {
	if f.inspect == nil {
		return nil, nil
	}
	return f.inspect[nameOrID], nil
}

// All other methods panic if called — they are not used by Build()
func (f *fakeRuntime) Name() string { panic("not implemented") }
func (f *fakeRuntime) Ping(ctx context.Context) error { panic("not implemented") }
func (f *fakeRuntime) Close() error { panic("not implemented") }
func (f *fakeRuntime) Pull(ctx context.Context, image string) error { panic("not implemented") }
func (f *fakeRuntime) CreateContainer(ctx context.Context, spec rt.ContainerSpec) (string, error) { panic("not implemented") }
func (f *fakeRuntime) StartContainer(ctx context.Context, id string) error { panic("not implemented") }
func (f *fakeRuntime) StopContainer(ctx context.Context, id string, timeout time.Duration) error { panic("not implemented") }
func (f *fakeRuntime) RemoveContainer(ctx context.Context, id string, force bool) error { panic("not implemented") }
func (f *fakeRuntime) Logs(ctx context.Context, id string, opts rt.LogOptions) (io.ReadCloser, error) { panic("not implemented") }
func (f *fakeRuntime) CreateNetwork(ctx context.Context, spec rt.NetworkSpec) (string, error) { panic("not implemented") }
func (f *fakeRuntime) RemoveNetwork(ctx context.Context, nameOrID string) error { panic("not implemented") }
func (f *fakeRuntime) NetworkExists(ctx context.Context, name string) (bool, error) { panic("not implemented") }
func (f *fakeRuntime) ListImages(ctx context.Context) ([]rt.ImageInfo, error) { panic("not implemented") }
func (f *fakeRuntime) RemoveImage(ctx context.Context, id string, force bool) error { panic("not implemented") }
func (f *fakeRuntime) ListVolumes(ctx context.Context, f2 rt.Filters) ([]rt.VolumeInfo, error) { panic("not implemented") }
func (f *fakeRuntime) RemoveVolume(ctx context.Context, name string, force bool) error { panic("not implemented") }
func (f *fakeRuntime) VolumeSizes(ctx context.Context) (map[string]int64, error) { panic("not implemented") }
func (f *fakeRuntime) LocalImageMeta(ctx context.Context, image string) (rt.ImageMeta, error) { panic("not implemented") }
func (f *fakeRuntime) RemoteImageDigest(ctx context.Context, image string) (string, error) { panic("not implemented") }
func (f *fakeRuntime) CheckTagUpdates(ctx context.Context, image string, max int) (*registry.TagUpdates, error) {
	panic("not implemented")
}
func (f *fakeRuntime) ContainerStats(ctx context.Context, id string) (rt.ContainerUsage, error) { panic("not implemented") }
func (f *fakeRuntime) EngineVersion(ctx context.Context) (rt.EngineInfo, error) { panic("not implemented") }
func (f *fakeRuntime) Exec(ctx context.Context, id string, opts rt.ExecOptions) (int, error) { panic("not implemented") }

// helper to create a managed container with proper labels
func managedContainer(name, image, hash string) rt.ContainerInfo {
	return rt.ContainerInfo{
		ID:    "ctr-" + name,
		Name:  "proj_" + name,
		Image: image,
		Labels: map[string]string{
			rt.LabelManaged:    "true",
			rt.LabelProject:    "proj",
			rt.LabelName:       name,
			rt.LabelConfigHash: hash,
		},
	}
}

func managedNetwork(name string) rt.NetworkInfo {
	return rt.NetworkInfo{
		ID:   "net-" + name,
		Name: "proj_" + name,
		Labels: map[string]string{
			rt.LabelManaged: "true",
			rt.LabelProject: "proj",
			rt.LabelName:    name,
		},
	}
}

func TestBuild_BasicCreate(t *testing.T) {
	stack := &config.Stack{
		Project: "proj",
		Containers: []config.Container{
			{Name: "web", Image: "nginx:1.27"},
		},
	}

	r := &fakeRuntime{} // nothing running

	plan, err := Build(context.Background(), stack, r, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.Containers) != 1 {
		t.Fatalf("expected 1 container action, got %d", len(plan.Containers))
	}
	if plan.Containers[0].Action != ActionCreate {
		t.Errorf("expected create, got %s", plan.Containers[0].Action)
	}
}

func TestBuild_SkipWhenHashMatches(t *testing.T) {
	c := config.Container{Name: "web", Image: "nginx:1.27"}
	hash := config.Hash(&c)

	stack := &config.Stack{
		Project:    "proj",
		Containers: []config.Container{c},
	}

	r := &fakeRuntime{
		containers: []rt.ContainerInfo{managedContainer("web", "nginx:1.27", hash)},
	}

	plan, err := Build(context.Background(), stack, r, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.Containers[0].Action != ActionSkip {
		t.Errorf("expected skip when hash matches, got %s", plan.Containers[0].Action)
	}
}

func TestBuild_RecreateWhenHashChanges(t *testing.T) {
	c := config.Container{Name: "web", Image: "nginx:1.27"}
	oldHash := "sha256:oldhash"

	stack := &config.Stack{
		Project:    "proj",
		Containers: []config.Container{c},
	}

	r := &fakeRuntime{
		containers: []rt.ContainerInfo{managedContainer("web", "nginx:1.27", oldHash)},
	}

	plan, err := Build(context.Background(), stack, r, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.Containers[0].Action != ActionRecreate {
		t.Errorf("expected recreate when hash differs, got %s", plan.Containers[0].Action)
	}
}

func TestBuild_DisabledInYAML(t *testing.T) {
	stack := &config.Stack{
		Project: "proj",
		Containers: []config.Container{
			{Name: "old", Image: "nginx:1", Disabled: true},
		},
	}

	r := &fakeRuntime{
		containers: []rt.ContainerInfo{managedContainer("old", "nginx:1", "whatever")},
	}

	plan, err := Build(context.Background(), stack, r, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.Containers[0].Action != ActionRemove {
		t.Errorf("expected remove for disabled: true, got %s", plan.Containers[0].Action)
	}
}

func TestBuild_StateFileDisabled(t *testing.T) {
	stack := &config.Stack{
		Project: "proj",
		Containers: []config.Container{
			{Name: "paused", Image: "redis:7"},
		},
	}

	r := &fakeRuntime{
		containers: []rt.ContainerInfo{managedContainer("paused", "redis:7", config.Hash(&stack.Containers[0]))},
	}

	disabled := map[string]bool{"paused": true}

	plan, err := Build(context.Background(), stack, r, nil, disabled)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plan.Containers[0].Action != ActionDisabled {
		t.Errorf("expected disabled action, got %s", plan.Containers[0].Action)
	}
}

func TestBuild_OrphanRemovalOnlyOnFullApply(t *testing.T) {
	stack := &config.Stack{Project: "proj"} // no containers declared

	r := &fakeRuntime{
		containers: []rt.ContainerInfo{managedContainer("leftover", "alpine", "abc")},
	}

	// Full apply (no names) → should remove orphan
	plan, _ := Build(context.Background(), stack, r, nil, nil)
	foundRemove := false
	for _, a := range plan.Containers {
		if a.Action == ActionRemove && a.Name == "leftover" {
			foundRemove = true
		}
	}
	if !foundRemove {
		t.Error("expected orphan removal on full apply")
	}

	// Partial apply → should NOT touch orphans
	plan2, _ := Build(context.Background(), stack, r, []string{"something"}, nil)
	for _, a := range plan2.Containers {
		if a.Name == "leftover" {
			t.Error("should not touch orphans during partial apply")
		}
	}
}

func TestBuild_UnmanagedConflict(t *testing.T) {
	stack := &config.Stack{
		Project: "proj",
		Containers: []config.Container{{Name: "db", Image: "postgres:16"}},
	}

	r := &fakeRuntime{
		inspect: map[string]*rt.ContainerInfo{
			"proj_db": {
				Labels: map[string]string{
					rt.LabelManaged: "false", // unmanaged
				},
			},
		},
	}

	_, err := Build(context.Background(), stack, r, nil, nil)
	if err == nil {
		t.Error("expected error for unmanaged container conflict")
	}
}

func TestBuild_DependsOnWarning(t *testing.T) {
	stack := &config.Stack{
		Project: "proj",
		Containers: []config.Container{
			{Name: "app", Image: "app:1", DependsOn: []string{"db"}},
			{Name: "db", Image: "db:1", Disabled: true},
		},
	}

	plan, err := Build(context.Background(), stack, &fakeRuntime{}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.Warnings) == 0 {
		t.Error("expected warning for depends_on disabled container")
	}
}

func TestBuild_CycleDetection(t *testing.T) {
	stack := &config.Stack{
		Project: "proj",
		Containers: []config.Container{
			{Name: "a", Image: "x", DependsOn: []string{"b"}},
			{Name: "b", Image: "x", DependsOn: []string{"a"}},
		},
	}

	_, err := Build(context.Background(), stack, &fakeRuntime{}, nil, nil)
	if err == nil {
		t.Error("expected cycle detection error")
	}
}

func TestBuild_NetworkCreateAndOrphan(t *testing.T) {
	stack := &config.Stack{
		Project: "proj",
		Networks: []config.Network{{Name: "backend"}},
	}

	r := &fakeRuntime{
		networks: []rt.NetworkInfo{managedNetwork("oldnet")},
	}

	plan, err := Build(context.Background(), stack, r, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasCreate := false
	hasRemove := false
	for _, n := range plan.Networks {
		if n.Name == "backend" && n.Action == ActionCreate {
			hasCreate = true
		}
		if n.Name == "oldnet" && n.Action == ActionRemove {
			hasRemove = true
		}
	}
	if !hasCreate {
		t.Error("expected network create for declared network")
	}
	if !hasRemove {
		t.Error("expected removal of orphaned managed network on full apply")
	}
}

func TestBuild_PartialApplyNameFilter(t *testing.T) {
	stack := &config.Stack{
		Project: "proj",
		Containers: []config.Container{
			{Name: "web", Image: "nginx"},
			{Name: "db", Image: "postgres"},
		},
	}

	r := &fakeRuntime{}

	plan, _ := Build(context.Background(), stack, r, []string{"web"}, nil)

	if len(plan.Containers) != 1 || plan.Containers[0].Name != "web" {
		t.Errorf("partial apply should only plan the named container, got %+v", plan.Containers)
	}
}

func TestBuild_TopoSortOrder(t *testing.T) {
	stack := &config.Stack{
		Project: "proj",
		Containers: []config.Container{
			{Name: "app", Image: "app:1", DependsOn: []string{"db", "cache"}},
			{Name: "cache", Image: "redis"},
			{Name: "db", Image: "postgres"},
		},
	}

	plan, _ := Build(context.Background(), stack, &fakeRuntime{}, nil, nil)

	order := []string{}
	for _, c := range plan.Containers {
		order = append(order, c.Name)
	}

	// db and cache must come before app
	dbIdx, cacheIdx, appIdx := -1, -1, -1
	for i, n := range order {
		if n == "db" { dbIdx = i }
		if n == "cache" { cacheIdx = i }
		if n == "app" { appIdx = i }
	}

	if !(dbIdx < appIdx && cacheIdx < appIdx) {
		t.Errorf("expected db and cache before app, got order: %v", order)
	}
}
