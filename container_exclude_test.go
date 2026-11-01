package main

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestInitExcludedContainers(t *testing.T) {
	os.Setenv("EXCLUDE_CONTAINERS", "")
	initExcludedContainers()
	if len(excludedContainerNames) != 0 {
		t.Errorf("expected 0, got %v", len(excludedContainerNames))
	}

	os.Setenv("EXCLUDE_CONTAINERS", " redis, , proxy ")
	initExcludedContainers()
	if len(excludedContainerNames) != 2 || excludedContainerNames[0] != "redis" || excludedContainerNames[1] != "proxy" {
		t.Errorf("expected [redis, proxy], got %v", excludedContainerNames)
	}
}

func TestIsLightHouseSelfContainer(t *testing.T) {
	if !isLightHouseSelfContainer("lighthouse", "nginx:latest") {
		t.Error("expected lighthouse name match")
	}
	if !isLightHouseSelfContainer("/lighthouse", "") {
		t.Error("expected trimmed lighthouse name match")
	}
	if !isLightHouseSelfContainer("api", "aimldev/lighthouse:latest") {
		t.Error("expected lighthouse image match")
	}
	if isLightHouseSelfContainer("api", "nginx:latest") {
		t.Error("expected non-lighthouse container to be false")
	}
}

func TestIsExcludedContainer(t *testing.T) {
	excludedContainerNames = []string{"redis", "proxy", ""}

	if !isExcludedContainer("lighthouse", "nginx:latest") {
		t.Error("lighthouse self must always be excluded")
	}
	if !isExcludedContainer("redis", "redis:7") {
		t.Error("expected redis in exclude list")
	}
	if !isExcludedContainer("/proxy", "nginx:latest") {
		t.Error("expected proxy in exclude list")
	}
	if isExcludedContainer("api", "node:20") {
		t.Error("api should not be excluded")
	}
}

func TestSanitizeContainerEnv(t *testing.T) {
	env := []string{
		"NORMAL=123",
		"NO_EQUAL_SIGN",
		"PASSWORD=secret",
		"DB_PASSWORD=secret",
		"SECRET_KEY=123",
		"TOKEN_ID=456",
		"AUTH_USER=abc",
		"PWD=/home",
		"DB_HOST=localhost",
	}

	expected := []string{
		"NORMAL=123",
		"NO_EQUAL_SIGN",
		"PASSWORD=••••••••••••",
		"DB_PASSWORD=••••••••••••",
		"SECRET_KEY=••••••••••••",
		"TOKEN_ID=••••••••••••",
		"AUTH_USER=••••••••••••",
		"PWD=••••••••••••",
		"DB_HOST=••••••••••••",
	}

	result := sanitizeContainerEnv(env)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestSanitizeContainerInspectMasksParsedAndRawPayloads(t *testing.T) {
	raw := json.RawMessage(`{"Id":"test-container","Config":{"Env":["VERIFY_SECRET=must-not-leak","PUBLIC_VALUE=visible"]}}`)
	var result client.ContainerInspectResult
	require.NoError(t, json.Unmarshal(raw, &result.Container))
	result.Raw = raw
	sanitizeContainerInspect(&result)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), "must-not-leak"), "inspect leaked a secret through parsed or raw data")
	require.Contains(t, string(encoded), "PUBLIC_VALUE=visible")
	require.Contains(t, string(result.Raw), "PUBLIC_VALUE=visible")
	require.NotEmpty(t, result.Raw)
}

func TestSanitizeInspectMapMasksEveryRepresentation(t *testing.T) {
	for _, payload := range []string{
		`{"Config":{"Env":["VERIFY_SECRET=must-not-leak","PUBLIC_VALUE=visible"]}}`,
		`{"Container":{"Config":{"Env":["VERIFY_SECRET=must-not-leak","PUBLIC_VALUE=visible"]}},"Raw":{"Config":{"Env":["VERIFY_SECRET=must-not-leak","PUBLIC_VALUE=visible"]}}}`,
		`{"Container":{"Config":{"Env":["PUBLIC_VALUE=visible"]}},"Raw":"must-not-leak"}`,
	} {
		var result map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(payload), &result))
		sanitizeInspectMap(result)
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		require.False(t, strings.Contains(string(encoded), "must-not-leak"), "remote inspect leaked a secret")
		require.Contains(t, string(encoded), "PUBLIC_VALUE=visible")
	}
}

func TestContainerNameImageFromInspect(t *testing.T) {
	name, img := containerNameImageFromInspect("/mycontainer", "myimage:latest")
	if name != "mycontainer" || img != "myimage:latest" {
		t.Errorf("expected mycontainer and myimage:latest, got %v and %v", name, img)
	}
}

func TestInspectContainerExcluded(t *testing.T) {
	excludedContainerNames = []string{"redis"}

	// Admin bypasses
	if inspectContainerExcluded(true, "redis", "redis:7") {
		t.Error("admin should not exclude")
	}

	// Non-admin lighthouse self
	if inspectContainerExcluded(false, "lighthouse", "nginx") {
		t.Error("non-admin should NOT exclude lighthouse self from inspect according to current logic")
	}

	// Non-admin excluded list
	if !inspectContainerExcluded(false, "redis", "redis:7") {
		t.Error("non-admin should exclude redis")
	}

	// Non-admin normal container
	if inspectContainerExcluded(false, "api", "node") {
		t.Error("non-admin should not exclude normal container")
	}
}
