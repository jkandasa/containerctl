package copy

import "testing"

func TestParseArg(t *testing.T) {
	tests := []struct {
		arg  string
		want Spec
	}{
		{"-", Spec{Path: "-"}},
		{"/abs/file", Spec{Path: "/abs/file"}},
		{"./rel:colon", Spec{Path: "./rel:colon"}},
		{"../up", Spec{Path: "../up"}},
		{"postgres:/var/lib/data", Spec{Container: "postgres", Path: "/var/lib/data"}},
		{"app:./local", Spec{Container: "app", Path: "./local"}},
		{"app:", Spec{Container: "app", Path: ""}},
		{"plainfile", Spec{Path: "plainfile"}},
		{"dir/file:name", Spec{Path: "dir/file:name"}},
	}
	for _, tc := range tests {
		got := ParseArg(tc.arg)
		if got != tc.want {
			t.Errorf("ParseArg(%q) = %+v, want %+v", tc.arg, got, tc.want)
		}
	}
}

func TestResolve(t *testing.T) {
	src, dest, dir, err := Resolve("postgres:/etc/hostname", "./hostname")
	if err != nil {
		t.Fatal(err)
	}
	if dir != DirFromContainer || src.Container != "postgres" || dest.Path != "./hostname" {
		t.Fatalf("from-container: src=%+v dest=%+v dir=%d", src, dest, dir)
	}

	src, dest, dir, err = Resolve("./config.yaml", "app:/etc/app/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if dir != DirToContainer || dest.Container != "app" || src.Path != "./config.yaml" {
		t.Fatalf("to-container: src=%+v dest=%+v dir=%d", src, dest, dir)
	}

	src, dest, dir, err = Resolve("postgres:/dump.sql", "app:/backup/dump.sql")
	if err != nil {
		t.Fatal(err)
	}
	if dir != DirAcrossContainers || src.Container != "postgres" || dest.Container != "app" {
		t.Fatalf("across: src=%+v dest=%+v dir=%d", src, dest, dir)
	}
	if _, _, _, err := Resolve("a:", "b:/y"); err == nil {
		t.Error("expected error for empty source container path")
	}
	if _, _, _, err := Resolve("./a", "./b"); err == nil {
		t.Error("expected error for host-to-host")
	}
	if _, _, _, err := Resolve("", "app:/x"); err == nil {
		t.Error("expected error for empty source")
	}
	if _, _, _, err := Resolve("app:", "./x"); err == nil {
		t.Error("expected error for empty container path")
	}
	if _, _, _, err := Resolve("./x", "app:"); err == nil {
		t.Error("expected error for empty dest container path")
	}
}
