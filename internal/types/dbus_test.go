package types

import (
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestDbusPolicy_UnmarshalYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want DbusPolicy
	}{
		{
			name: "legacy bool true maps to full",
			in:   "dbus: true",
			want: DbusPolicy{Mode: DbusFull},
		},
		{
			name: "legacy bool false maps to none",
			in:   "dbus: false",
			want: DbusPolicy{Mode: DbusNone},
		},
		{
			name: "struct with rules",
			in: `dbus:
  mode: filtered
  session:
    - talk=org.freedesktop.Notifications
  system:
    - see=org.freedesktop.UPower`,
			want: DbusPolicy{
				Mode:    DbusFiltered,
				Session: []string{"talk=org.freedesktop.Notifications"},
				System:  []string{"see=org.freedesktop.UPower"},
			},
		},
		{
			name: "struct without mode defaults to none",
			in: `dbus:
  session:
    - talk=org.freedesktop.Notifications`,
			want: DbusPolicy{
				Mode:    DbusNone,
				Session: []string{"talk=org.freedesktop.Notifications"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var doc struct {
				Dbus DbusPolicy `yaml:"dbus"`
			}
			if err := yaml.Unmarshal([]byte(tt.in), &doc); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got := doc.Dbus
			if got.Mode != tt.want.Mode {
				t.Errorf("mode: got %q want %q", got.Mode, tt.want.Mode)
			}
			if !equalStrings(got.Session, tt.want.Session) {
				t.Errorf("session: got %v want %v", got.Session, tt.want.Session)
			}
			if !equalStrings(got.System, tt.want.System) {
				t.Errorf("system: got %v want %v", got.System, tt.want.System)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseDbusRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    dbusRule
		wantErr bool
	}{
		{name: "talk", in: "talk=org.freedesktop.Notifications",
			want: dbusRule{verb: "talk", name: "org.freedesktop.Notifications"}},
		{name: "see wildcard", in: "see=org.freedesktop.*",
			want: dbusRule{verb: "see", name: "org.freedesktop.*"}},
		{name: "own", in: "own=com.example.App",
			want: dbusRule{verb: "own", name: "com.example.App"}},
		{name: "call with method", in: "call=org.foo.Bar=org.foo.Bar.Method",
			want: dbusRule{verb: "call", name: "org.foo.Bar", method: "org.foo.Bar.Method"}},
		{name: "broadcast with method", in: "broadcast=org.foo.Bar=org.foo.Bar.Signal",
			want: dbusRule{verb: "broadcast", name: "org.foo.Bar", method: "org.foo.Bar.Signal"}},
		{name: "unknown verb", in: "poke=org.foo.Bar", wantErr: true},
		{name: "empty name", in: "talk=", wantErr: true},
		{name: "call without method", in: "call=org.foo.Bar", wantErr: true},
		{name: "talk with method", in: "talk=org.foo.Bar=x", wantErr: true},
		{name: "no equals", in: "talk", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseDbusRule(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v want %+v", got, tt.want)
			}
		})
	}
}

func TestDbusPolicy_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		policy  DbusPolicy
		wantErr bool
	}{
		{name: "empty mode is valid (zero value)", policy: DbusPolicy{}},
		{name: "none is valid", policy: DbusPolicy{Mode: DbusNone}},
		{name: "full is valid", policy: DbusPolicy{Mode: DbusFull}},
		{name: "filtered with valid rules", policy: DbusPolicy{
			Mode:    DbusFiltered,
			Session: []string{"talk=org.freedesktop.Notifications"},
			System:  []string{"see=org.freedesktop.UPower"},
		}},
		{name: "unknown mode", policy: DbusPolicy{Mode: "weird"}, wantErr: true},
		{name: "filtered with invalid rule", policy: DbusPolicy{
			Mode:    DbusFiltered,
			Session: []string{"poke=org.foo"},
		}, wantErr: true},
		{name: "rules ignored when full", policy: DbusPolicy{
			Mode:    DbusFull,
			Session: []string{"poke=org.foo"},
		}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.policy.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDbusRuleCovers(t *testing.T) {
	t.Parallel()

	mk := func(s string) dbusRule {
		r, err := parseDbusRule(s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return r
	}

	tests := []struct {
		name    string
		ceiling string
		want    string
		covered bool
	}{
		{name: "exact talk", ceiling: "talk=org.foo.Bar", want: "talk=org.foo.Bar", covered: true},
		{name: "own covers talk", ceiling: "own=org.foo.Bar", want: "talk=org.foo.Bar", covered: true},
		{name: "talk covers see", ceiling: "talk=org.foo.Bar", want: "see=org.foo.Bar", covered: true},
		{name: "see does not cover talk", ceiling: "see=org.foo.Bar", want: "talk=org.foo.Bar", covered: false},
		{name: "subtree covers child", ceiling: "talk=org.foo.*", want: "talk=org.foo.Bar", covered: true},
		{name: "subtree covers parent name", ceiling: "talk=org.foo.*", want: "talk=org.foo", covered: true},
		{name: "subtree does not cross namespace", ceiling: "talk=org.foo.*", want: "talk=org.other", covered: false},
		{name: "talk covers call", ceiling: "talk=org.foo.Bar", want: "call=org.foo.Bar=org.foo.Bar.M", covered: true},
		{name: "call covers exact call", ceiling: "call=org.foo.Bar=org.foo.Bar.M", want: "call=org.foo.Bar=org.foo.Bar.M", covered: true},
		{name: "call subtree covers method", ceiling: "call=org.foo.Bar=org.foo.Bar.*", want: "call=org.foo.Bar=org.foo.Bar.M", covered: true},
		{name: "call does not cover different method", ceiling: "call=org.foo.Bar=org.foo.Bar.M", want: "call=org.foo.Bar=org.foo.Bar.N", covered: false},
		{name: "see does not cover call", ceiling: "see=org.foo.Bar", want: "call=org.foo.Bar=org.foo.Bar.M", covered: false},
		{name: "talk covers broadcast", ceiling: "talk=org.foo.Bar", want: "broadcast=org.foo.Bar=org.foo.Bar.S", covered: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ruleCovers(mk(tt.ceiling), mk(tt.want))
			if got != tt.covered {
				t.Errorf("ruleCovers(%q, %q) = %v want %v", tt.ceiling, tt.want, got, tt.covered)
			}
		})
	}
}

func TestMergeDbus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		workload DbusPolicy
		profile  DbusPolicy
		want     DbusPolicy
	}{
		{
			name:     "both full stays full",
			workload: DbusPolicy{Mode: DbusFull},
			profile:  DbusPolicy{Mode: DbusFull},
			want:     DbusPolicy{Mode: DbusFull},
		},
		{
			name:     "profile none forces none",
			workload: DbusPolicy{Mode: DbusFull},
			profile:  DbusPolicy{Mode: DbusNone},
			want:     DbusPolicy{Mode: DbusNone},
		},
		{
			name:     "workload none forces none",
			workload: DbusPolicy{Mode: DbusNone},
			profile:  DbusPolicy{Mode: DbusFull},
			want:     DbusPolicy{Mode: DbusNone},
		},
		{
			name:     "profile full, workload filtered keeps workload rules",
			workload: DbusPolicy{Mode: DbusFiltered, Session: []string{"talk=org.foo.Bar"}},
			profile:  DbusPolicy{Mode: DbusFull},
			want:     DbusPolicy{Mode: DbusFiltered, Session: []string{"talk=org.foo.Bar"}},
		},
		{
			name:     "profile filtered, workload full bounded to profile rules",
			workload: DbusPolicy{Mode: DbusFull},
			profile:  DbusPolicy{Mode: DbusFiltered, Session: []string{"talk=org.foo.Bar"}},
			want:     DbusPolicy{Mode: DbusFiltered, Session: []string{"talk=org.foo.Bar"}},
		},
		{
			name: "both filtered intersects",
			workload: DbusPolicy{Mode: DbusFiltered, Session: []string{
				"talk=org.foo.Bar",        // covered by profile subtree
				"talk=org.denied.Service", // dropped
			}},
			profile: DbusPolicy{Mode: DbusFiltered, Session: []string{"talk=org.foo.*"}},
			want:    DbusPolicy{Mode: DbusFiltered, Session: []string{"talk=org.foo.Bar"}},
		},
		{
			name:     "intersection drops over-broad workload permission",
			workload: DbusPolicy{Mode: DbusFiltered, Session: []string{"own=org.foo.Bar"}},
			profile:  DbusPolicy{Mode: DbusFiltered, Session: []string{"talk=org.foo.Bar"}},
			want:     DbusPolicy{Mode: DbusFiltered, Session: nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := MergeDbus(tt.workload, tt.profile)
			if got.Mode != tt.want.Mode {
				t.Fatalf("mode: got %q want %q", got.Mode, tt.want.Mode)
			}
			if !equalStrings(got.Session, tt.want.Session) {
				t.Errorf("session: got %v want %v", got.Session, tt.want.Session)
			}
			if !equalStrings(got.System, tt.want.System) {
				t.Errorf("system: got %v want %v", got.System, tt.want.System)
			}
		})
	}
}

func TestProxyFlags(t *testing.T) {
	t.Parallel()

	got := ProxyFlags([]string{
		"talk=org.freedesktop.Notifications",
		"call=org.foo.Bar=org.foo.Bar.M",
	})
	want := []string{
		"--filter",
		"--talk=org.freedesktop.Notifications",
		"--call=org.foo.Bar=org.foo.Bar.M",
	}
	if !equalStrings(got, want) {
		t.Errorf("got %v want %v", got, want)
	}

	// Empty rules still deny-by-default via --filter.
	if got := ProxyFlags(nil); !equalStrings(got, []string{"--filter"}) {
		t.Errorf("empty: got %v want [--filter]", got)
	}
}

func TestIntersectRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		want    []string
		ceiling []string
		result  []string
	}{
		{
			name:    "nil ceiling drops everything",
			want:    []string{"talk=org.foo.Bar"},
			ceiling: nil,
			result:  nil,
		},
		{
			name:    "subtree ceiling keeps covered rules",
			want:    []string{"talk=org.foo.Bar", "talk=org.other"},
			ceiling: []string{"talk=org.foo.*"},
			result:  []string{"talk=org.foo.Bar"},
		},
		{
			name:    "unparseable ceiling rule is skipped",
			want:    []string{"talk=org.foo.Bar"},
			ceiling: []string{"poke=org.foo.Bar", "talk=org.foo.Bar"},
			result:  []string{"talk=org.foo.Bar"},
		},
		{
			name:    "unparseable want rule is skipped",
			want:    []string{"poke=org.foo.Bar", "talk=org.foo.Bar"},
			ceiling: []string{"talk=org.foo.*"},
			result:  []string{"talk=org.foo.Bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := intersectRules(tt.want, tt.ceiling)
			if !equalStrings(got, tt.result) {
				t.Errorf("got %v want %v", got, tt.result)
			}
		})
	}
}

func TestMergeDbus_BusesIntersectIndependently(t *testing.T) {
	t.Parallel()

	workload := DbusPolicy{
		Mode:    DbusFiltered,
		Session: []string{"talk=org.session.Svc"},
		System:  []string{"see=org.system.Svc"},
	}
	profile := DbusPolicy{
		Mode:    DbusFiltered,
		Session: []string{"talk=org.session.*"},
		System:  nil,
	}

	got := MergeDbus(workload, profile)
	if got.Mode != DbusFiltered {
		t.Fatalf("mode: got %q want filtered", got.Mode)
	}
	if !equalStrings(got.Session, []string{"talk=org.session.Svc"}) {
		t.Errorf("session: got %v want [talk=org.session.Svc]", got.Session)
	}
	if got.System != nil {
		t.Errorf("system: nil ceiling should drop all, got %v", got.System)
	}
}

func TestDbusEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		a, b  DbusPolicy
		equal bool
	}{
		{name: "unset equals explicit none", a: DbusPolicy{}, b: DbusPolicy{Mode: DbusNone}, equal: true},
		{name: "unset equals unset", a: DbusPolicy{}, b: DbusPolicy{}, equal: true},
		{name: "full differs from none", a: DbusPolicy{Mode: DbusFull}, b: DbusPolicy{Mode: DbusNone}, equal: false},
		{name: "full differs from unset", a: DbusPolicy{Mode: DbusFull}, b: DbusPolicy{}, equal: false},
		{
			name:  "same filtered rules equal",
			a:     DbusPolicy{Mode: DbusFiltered, Session: []string{"talk=a"}},
			b:     DbusPolicy{Mode: DbusFiltered, Session: []string{"talk=a"}},
			equal: true,
		},
		{
			name:  "different session rules differ",
			a:     DbusPolicy{Mode: DbusFiltered, Session: []string{"talk=a"}},
			b:     DbusPolicy{Mode: DbusFiltered, Session: []string{"talk=b"}},
			equal: false,
		},
		{
			name:  "system rules differ",
			a:     DbusPolicy{Mode: DbusFiltered, System: []string{"see=a"}},
			b:     DbusPolicy{Mode: DbusFiltered},
			equal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := DbusEqual(tt.a, tt.b); got != tt.equal {
				t.Errorf("DbusEqual(%+v, %+v) = %v want %v", tt.a, tt.b, got, tt.equal)
			}
		})
	}
}
