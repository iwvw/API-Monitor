package serveragent

import (
	"reflect"
	"testing"
)

func TestValidateDockerCreatePolicyAllowsSafePayload(t *testing.T) {
	payloads := []map[string]interface{}{
		{},
		{"privileged": false, "extraArgs": ""},
		{"privileged": false, "extraArgs": []string{"--restart", "unless-stopped", "-e", "FOO=bar"}},
		{"privileged": false, "extraArgs": []interface{}{"--memory", "256m", "  ", "--cpus", "1.5"}},
		{"privileged": false, "extraArgs": "--name demo"},
		{"privileged": false, "extra_args": []string{"--publish", "80:80"}},
	}
	for index, payload := range payloads {
		if violations := validateDockerCreatePolicy(payload); len(violations) != 0 {
			t.Fatalf("payload %d violations=%v, want none", index, violations)
		}
	}
}

func TestValidateDockerCreatePolicyRejectsPrivilegedFlag(t *testing.T) {
	payload := map[string]interface{}{"privileged": true}
	violations := validateDockerCreatePolicy(payload)
	if len(violations) != 1 || violations[0] != "privileged containers are not allowed" {
		t.Fatalf("violations=%v", violations)
	}
}

func TestValidateDockerCreatePolicyRejectsDangerousExtraArgs(t *testing.T) {
	cases := []struct {
		arg  string
		want string
	}{
		{"--privileged", "extraArgs cannot enable privileged mode"},
		{"--privileged=true", "extraArgs cannot enable privileged mode"},
		{"  --PRIVILEGED  ", "extraArgs cannot enable privileged mode"},
		{"--pid=host", "extraArgs cannot use host PID namespace"},
		{"--pid host", "extraArgs cannot use host PID namespace"},
		{"--network=host", "extraArgs cannot use host network namespace"},
		{"--net=host", "extraArgs cannot use host network namespace"},
		{"--network host", "extraArgs cannot use host network namespace"},
		{"--net host", "extraArgs cannot use host network namespace"},
		{"--device=/dev/sda", "extraArgs cannot map host devices"},
		{"--device", "extraArgs cannot map host devices"},
		{"--cap-add=NET_ADMIN", "extraArgs cannot add Linux capabilities"},
		{"--cap-add ALL", "extraArgs cannot add Linux capabilities"},
		{"--security-opt=no-new-privileges", "extraArgs cannot set security options"},
		{"--sysctl=net.ipv4.ip_forward=1", "extraArgs cannot set sysctls"},
	}
	for _, tc := range cases {
		payload := map[string]interface{}{"extraArgs": []string{tc.arg}}
		violations := validateDockerCreatePolicy(payload)
		if len(violations) != 1 || violations[0] != tc.want {
			t.Fatalf("arg %q violations=%v, want %q", tc.arg, violations, tc.want)
		}
	}
}

func TestValidateDockerCreatePolicyDedupesEmptyArgs(t *testing.T) {
	payload := map[string]interface{}{"extraArgs": []interface{}{"", "   ", "--privileged", ""}}
	violations := validateDockerCreatePolicy(payload)
	if len(violations) != 1 {
		t.Fatalf("violations=%v, want exactly one", violations)
	}
}

func TestStringListFromPayload(t *testing.T) {
	empty := map[string]interface{}{"extraArgs": []string{"", "  "}}
	if got := stringListFromPayload(empty["extraArgs"]); !reflect.DeepEqual(got, []string{"", "  "}) {
		t.Fatalf("string slice = %#v, want raw slice", got)
	}
	cases := []struct {
		name  string
		value interface{}
		want  []string
	}{
		{"string list", []string{"a", "b"}, []string{"a", "b"}},
		{"interface list", []interface{}{"a", 42, "", " c "}, []string{"a", "42", "c"}},
		{"single string", "hello", []string{"hello"}},
		{"blank string", "  ", nil},
		{"nil", nil, nil},
		{"int", 7, nil},
		{"bool", true, nil},
	}
	for _, tc := range cases {
		if got := stringListFromPayload(tc.value); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s = %#v, want %#v", tc.name, got, tc.want)
		}
	}
	if payload := map[string]interface{}{"extraArgs": []interface{}{"a", "b"}}; len(validateDockerCreatePolicy(payload)) != 0 {
		t.Fatal("benign interface list should pass policy")
	}
}
