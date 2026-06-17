package main

import "testing"

func TestIsLightHouseSelfContainer(t *testing.T) {
	if !isLightHouseSelfContainer("lighthouse", "nginx:latest") {
		t.Fatal("expected lighthouse name match")
	}
	if !isLightHouseSelfContainer("/lighthouse", "") {
		t.Fatal("expected trimmed lighthouse name match")
	}
	if !isLightHouseSelfContainer("api", "aimldev/lighthouse:latest") {
		t.Fatal("expected lighthouse image match")
	}
	if isLightHouseSelfContainer("api", "nginx:latest") {
		t.Fatal("expected non-lighthouse container to be false")
	}
}

func TestIsExcludedContainer(t *testing.T) {
	excludedContainerNames = []string{"redis", "proxy"}

	if !isExcludedContainer("lighthouse", "nginx:latest") {
		t.Fatal("lighthouse self must always be excluded")
	}
	if !isExcludedContainer("redis", "redis:7") {
		t.Fatal("expected redis in exclude list")
	}
	if !isExcludedContainer("proxy", "nginx:latest") {
		t.Fatal("expected proxy in exclude list")
	}
	if isExcludedContainer("api", "node:20") {
		t.Fatal("api should not be excluded")
	}
}
