package cmd

import (
	"reflect"
	"testing"

	"github.com/jkandasa/containerctl/internal/config"
)

func testStack() *config.Stack {
	return &config.Stack{
		Project: "p",
		Containers: []config.Container{
			{Name: "postgres", Labels: map[string]string{"app": "db", "tier": "backend", "environment": "production", "release": "v1"}},
			{Name: "redis", Labels: map[string]string{"app": "cache", "tier": "backend", "environment": "production"}},
			{Name: "web", Labels: map[string]string{"app": "frontend", "tier": "frontend", "environment": "production", "release": "v1"}},
			{Name: "web-dev", Labels: map[string]string{"app": "frontend", "tier": "frontend", "environment": "development", "release": "v1"}},
			{Name: "plain"}, // no labels
		},
	}
}

func TestParseLabelFilters(t *testing.T) {
	got, err := parseLabelFilters([]string{"tier=backend", "role=db"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "tier" || got[0].Op != labelOpEq || got[0].Value != "backend" {
		t.Errorf("got %+v", got)
	}
	if got[1].Key != "role" || got[1].Value != "db" {
		t.Errorf("got %+v", got)
	}

	// comma-separated + !=
	got, err = parseLabelFilters([]string{"app=frontend,environment!=development"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d selectors, want 2: %+v", len(got), got)
	}
	if got[0].Key != "app" || got[0].Op != labelOpEq || got[0].Value != "frontend" {
		t.Errorf("sel0=%+v", got[0])
	}
	if got[1].Key != "environment" || got[1].Op != labelOpNeq || got[1].Value != "development" {
		t.Errorf("sel1=%+v", got[1])
	}

	// kubectl-style: KEY, KEY=VALUE
	got, err = parseLabelFilters([]string{"release,environment=production"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d: %+v", len(got), got)
	}
	if got[0].Key != "release" || got[0].Op != labelOpExists {
		t.Errorf("sel0=%+v want Exists release", got[0])
	}
	if got[1].Key != "environment" || got[1].Op != labelOpEq || got[1].Value != "production" {
		t.Errorf("sel1=%+v", got[1])
	}

	// !KEY
	got, err = parseLabelFilters([]string{"!debug"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Op != labelOpNotExists || got[0].Key != "debug" {
		t.Errorf("got %+v", got)
	}

	if _, err := parseLabelFilters([]string{"=value"}); err == nil {
		t.Error("expected error for empty key")
	}
	if _, err := parseLabelFilters([]string{"!=value"}); err == nil {
		t.Error("expected error for empty key on !=")
	}
	// empty value is allowed
	got, err = parseLabelFilters([]string{"empty="})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Value != "" || got[0].Op != labelOpEq {
		t.Errorf("empty value: %+v", got)
	}
}

func TestSelectContainerNames_LabelOnly(t *testing.T) {
	s := testStack()
	names, filtered, err := selectContainerNames(s, nil, []string{"tier=backend"}, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !filtered {
		t.Fatal("expected filtered")
	}
	if !reflect.DeepEqual(names, []string{"postgres", "redis"}) {
		t.Errorf("got %v", names)
	}
}

func TestSelectContainerNames_CommaAndNotEqual(t *testing.T) {
	s := testStack()
	// -l app=frontend,environment!=development  → web (not web-dev)
	names, _, err := selectContainerNames(s, nil, []string{"app=frontend,environment!=development"}, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"web"}) {
		t.Errorf("got %v want [web]", names)
	}
}

func TestSelectContainerNames_ExistsAndEquals(t *testing.T) {
	s := testStack()
	// kubectl: -l release,environment=production
	// postgres and web have release + production; redis is production but no release; web-dev has release but development
	names, _, err := selectContainerNames(s, nil, []string{"release,environment=production"}, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"postgres", "web"}) {
		t.Errorf("got %v want [postgres web]", names)
	}
}

func TestSelectContainerNames_NotExists(t *testing.T) {
	s := testStack()
	// -l !release → redis (production, no release) and plain
	names, _, err := selectContainerNames(s, nil, []string{"!release"}, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"plain", "redis"}) {
		t.Errorf("got %v want [plain redis]", names)
	}
}

func TestSelectContainerNames_NotEqualMissingLabel(t *testing.T) {
	s := testStack()
	// plain has no environment label → matches environment!=development
	names, _, err := selectContainerNames(s, nil, []string{"environment!=development"}, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	// all except web-dev
	want := []string{"plain", "postgres", "redis", "web"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("got %v want %v", names, want)
	}
}

func TestSelectContainerNames_MultipleLabelsAND(t *testing.T) {
	s := testStack()
	names, _, err := selectContainerNames(s, nil, []string{"tier=backend", "app=db"}, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"postgres"}) {
		t.Errorf("got %v want [postgres]", names)
	}
}

func TestSelectContainerNames_NamesAndLabelsIntersection(t *testing.T) {
	s := testStack()
	names, _, err := selectContainerNames(s, []string{"postgres", "redis", "web"}, []string{"tier=backend"}, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"postgres", "redis"}) {
		t.Errorf("got %v", names)
	}
}

func TestSelectContainerNames_Unfiltered(t *testing.T) {
	s := testStack()
	names, filtered, err := selectContainerNames(s, nil, nil, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if filtered || names != nil {
		t.Errorf("expected unfiltered, got names=%v filtered=%v", names, filtered)
	}
}

func TestSelectContainerNames_RequireSelect(t *testing.T) {
	s := testStack()
	_, _, err := selectContainerNames(s, nil, nil, true, false, false)
	if err == nil {
		t.Fatal("expected error when requireSelect and nothing given")
	}
	// --all alone is ok
	names, filtered, err := selectContainerNames(s, nil, nil, true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !filtered || len(names) != 5 {
		t.Errorf("got names=%v filtered=%v", names, filtered)
	}
}

func TestSelectContainerNames_AllWithLabels(t *testing.T) {
	s := testStack()
	names, _, err := selectContainerNames(s, nil, []string{"tier=frontend"}, true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"web", "web-dev"}) {
		t.Errorf("got %v", names)
	}
}

func TestSelectContainerNames_NoMatch(t *testing.T) {
	s := testStack()
	_, _, err := selectContainerNames(s, nil, []string{"tier=missing"}, false, false, false)
	if err == nil {
		t.Fatal("expected error when nothing matches")
	}
}

func TestSelectContainerNames_UnknownName(t *testing.T) {
	s := testStack()
	_, _, err := selectContainerNames(s, []string{"nope"}, nil, false, false, false)
	if err == nil {
		t.Fatal("expected error for unknown name")
	}
}

func TestSelectContainerNames_AllowUnknownNames(t *testing.T) {
	s := testStack()
	names, _, err := selectContainerNames(s, []string{"nope", "web"}, nil, false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"nope", "web"}) {
		t.Errorf("got %v", names)
	}
}
