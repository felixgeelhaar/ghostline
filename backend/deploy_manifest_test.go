package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The cluster deployment exists twice: rollopsd applies the manifests inlined in
// .rollops/, and deploy/kubernetes/ghostline.yaml is the break-glass copy for
// kubectl when rollopsd is down. They must stay identical, and the Deployment must
// keep the properties the single-instance design depends on.

// releaseImage is a Ghostline release pinned by digest. rollopsd's image writeback
// rewrites only the tracked image line in .rollops/, so that line is checked for shape,
// not for one particular release.
var releaseImage = regexp.MustCompile(`^ghcr\.io/felixgeelhaar/ghostline:v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?@sha256:[0-9a-f]{64}$`)

// containerImage matches the container image line inside a manifest.
var containerImage = regexp.MustCompile(`(?m)^(\s+image: )\S+$`)

// breakGlassDocs maps "Kind/name" to each YAML document in the break-glass file.
func breakGlassDocs(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("../deploy/kubernetes/ghostline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]string{}
	for _, doc := range strings.Split(string(raw), "\n---\n") {
		doc = strings.TrimSpace(stripComments(doc)) + "\n"
		kind := regexp.MustCompile(`(?m)^kind: (\S+)$`).FindStringSubmatch(doc)
		name := regexp.MustCompile(`(?m)^  name: (\S+)$`).FindStringSubmatch(doc)
		if kind == nil || name == nil {
			t.Fatalf("break-glass document without kind or name:\n%s", doc)
		}
		docs[kind[1]+"/"+name[1]] = doc
	}
	return docs
}

func stripComments(doc string) string {
	var lines []string
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

type rolloutConfig struct {
	file, resource, image, manifest string
}

func rolloutConfigs(t *testing.T) []rolloutConfig {
	t.Helper()
	files, err := filepath.Glob("../.rollops/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no .rollops configs: %v", err)
	}
	var configs []rolloutConfig
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		resource := regexp.MustCompile(`(?m)^      resource: (\S+)$`).FindStringSubmatch(text)
		block := regexp.MustCompile(`(?m)^      manifest: \|\n((?:        .*\n|\n)+)`).FindStringSubmatch(text)
		if resource == nil || block == nil {
			t.Fatalf("%s: no resource or inline manifest", file)
		}
		var manifest []string
		for _, line := range strings.Split(strings.TrimRight(block[1], "\n"), "\n") {
			manifest = append(manifest, strings.TrimPrefix(line, "        "))
		}
		config := rolloutConfig{file: filepath.Base(file), resource: resource[1], manifest: strings.TrimSpace(stripComments(strings.Join(manifest, "\n"))) + "\n"}
		if image := regexp.MustCompile(`(?m)^      image: (\S+)$`).FindStringSubmatch(text); image != nil {
			config.image = image[1]
		}
		configs = append(configs, config)
	}
	return configs
}

// oneTimeSetup are the documents applied once from the break-glass file, outside rollops.
var oneTimeSetup = map[string]bool{
	"Namespace/ghostline": true, "NetworkPolicy/namespace-isolation": true,
	"ResourceQuota/ghostline": true, "LimitRange/ghostline": true,
}

func TestRolloutManifestsMatchTheBreakGlassCopy(t *testing.T) {
	docs := breakGlassDocs(t)
	kinds := map[string]string{"deployment": "Deployment", "service": "Service", "ingress": "Ingress", "middleware": "Middleware"}
	managed := map[string]bool{}
	for _, config := range rolloutConfigs(t) {
		kind, name, _ := strings.Cut(config.resource, "/")
		key := kinds[kind] + "/" + name
		doc, ok := docs[key]
		if !ok {
			t.Errorf("%s manages %s, which the break-glass file lacks", config.file, key)
			continue
		}
		// rollopsd renders the tracked image over the manifest's, so only that line may differ.
		if containerImage.ReplaceAllString(config.manifest, "${1}<tracked>") != containerImage.ReplaceAllString(doc, "${1}<tracked>") {
			t.Errorf("%s and the break-glass %s differ:\n--- rollops\n%s--- break-glass\n%s", config.file, key, config.manifest, doc)
		}
		managed[key] = true
	}
	for key := range docs {
		// The namespace and its isolation policy are one-time setup outside rollops.
		if !managed[key] && !oneTimeSetup[key] {
			t.Errorf("break-glass %s has no .rollops config", key)
		}
	}
}

func TestDeploymentKeepsTheSingleInstanceContract(t *testing.T) {
	deployment := breakGlassDocs(t)["Deployment/ghostline"]
	for _, want := range []string{
		// Sessions, OAuth state and rate windows live in memory (docs/hosted-deployment.md).
		"replicas: 1",
		"type: Recreate",
		"runAsNonRoot: true",
		"readOnlyRootFilesystem: true",
		"allowPrivilegeEscalation: false",
		"path: /api/health",
		"name: ghostline-env",
		// Pod and container both pin the default seccomp profile.
		"            seccompProfile:\n              type: RuntimeDefault",
		"value: https://ghostline.felixgeelhaar.de/auth/callback",
		// Traefik sets X-Real-Ip; only Traefik and observability can reach the pod.
		"value: X-Real-Ip",
	} {
		if !strings.Contains(deployment, want) {
			t.Errorf("Deployment lacks %q", want)
		}
	}
	if image := containerImage.FindStringSubmatch(deployment); image == nil || !releaseImage.MatchString(strings.TrimPrefix(image[0], image[1])) {
		t.Errorf("break-glass Deployment image must be a Ghostline release pinned by digest, got %v", image)
	}
	tracked := false
	for _, config := range rolloutConfigs(t) {
		if config.resource == "deployment/ghostline" {
			tracked = true
			if !releaseImage.MatchString(config.image) {
				t.Errorf("%s tracks image %q, want a Ghostline release pinned by digest", config.file, config.image)
			}
		}
	}
	if !tracked {
		t.Error("no .rollops config tracks deployment/ghostline")
	}

	grace := regexp.MustCompile(`(?m)GHOSTLINE_SHUTDOWN_GRACE\n\s+value: "?(\w+)"?`).FindStringSubmatch(deployment)
	stop := regexp.MustCompile(`(?m)terminationGracePeriodSeconds: (\d+)`).FindStringSubmatch(deployment)
	if grace == nil || stop == nil {
		t.Fatal("Deployment must set GHOSTLINE_SHUTDOWN_GRACE and terminationGracePeriodSeconds")
	}
	graceDuration, err := time.ParseDuration(grace[1])
	if err != nil {
		t.Fatal(err)
	}
	stopDuration, _ := time.ParseDuration(stop[1] + "s")
	if stopDuration < graceDuration+5*time.Second {
		t.Errorf("terminationGracePeriodSeconds %s leaves less than 5s beyond the %s shutdown grace", stopDuration, graceDuration)
	}
}

// The container reads only the keys it needs from ghostline-env, never the whole Secret.
func TestDeploymentReadsNamedSecretKeys(t *testing.T) {
	deployment := breakGlassDocs(t)["Deployment/ghostline"]
	if strings.Contains(deployment, "envFrom") {
		t.Error("Deployment loads the entire ghostline-env Secret with envFrom")
	}
	for _, key := range []string{"GHOSTLINE_SESSION_SECRET", "GITHUB_OAUTH_CLIENT_ID", "GITHUB_OAUTH_CLIENT_SECRET", "GHOSTLINE_ALLOWED_GITHUB_IDS", "GITHUB_TOKEN"} {
		if !strings.Contains(deployment, "key: "+key) {
			t.Errorf("Deployment does not read %s from ghostline-env", key)
		}
	}
}

func TestNamespaceBoundsItsResources(t *testing.T) {
	docs := breakGlassDocs(t)
	for _, key := range []string{"ResourceQuota/ghostline", "LimitRange/ghostline"} {
		if !strings.Contains(docs[key], "ghostline.felixgeelhaar.de/setup: one-time") {
			t.Errorf("break-glass file lacks a one-time %s", key)
		}
	}
}

// The app trusts X-Real-Ip from the pod network, so only Traefik's pods may connect:
// any other pod could choose its own rate-limit identity.
func TestOnlyTraefikCanReachThePod(t *testing.T) {
	policy := breakGlassDocs(t)["NetworkPolicy/namespace-isolation"]
	for _, want := range []string{"kubernetes.io/metadata.name: kube-system", "app.kubernetes.io/name: traefik", "app.kubernetes.io/instance: traefik-kube-system"} {
		if !strings.Contains(policy, want) {
			t.Errorf("NetworkPolicy lacks %q", want)
		}
	}
	// cert-manager's HTTP-01 solver pod runs in this namespace on 8089; without it
	// Traefik answers the ACME challenge with a 502 and the certificate never issues.
	for _, port := range []string{"port: 8080", "port: 8089"} {
		if !strings.Contains(policy, port) {
			t.Errorf("NetworkPolicy must admit Traefik on %s", port)
		}
	}
	if strings.Contains(policy, "observability") || strings.Count(policy, "namespaceSelector") != 1 {
		t.Error("NetworkPolicy must admit Traefik's pods and nothing else")
	}
	if !regexp.MustCompile(`(?m)^        - namespaceSelector:\n(?:            .*\n)+          podSelector:\n`).MatchString(policy) {
		t.Error("the namespace and pod selectors must be one peer (AND), not two (OR)")
	}
}

func TestIngressStripsClientRequestIDs(t *testing.T) {
	docs := breakGlassDocs(t)
	if !strings.Contains(docs["Middleware/edge-headers"], `X-Request-ID: ""`) {
		t.Error("edge-headers must remove a client's X-Request-ID; the app trusts Traefik's")
	}
	ingress := docs["Ingress/ghostline"]
	for _, want := range []string{"ghostline-edge-headers@kubernetescrd", "ghostline-https-redirect@kubernetescrd", "cert-manager.io/cluster-issuer: letsencrypt-prod", "host: ghostline.felixgeelhaar.de"} {
		if !strings.Contains(ingress, want) {
			t.Errorf("Ingress lacks %q", want)
		}
	}
}

// Secret values must never reach process arguments or shell history, so the documented
// setup reads them from stdin rather than --from-literal.
func TestSecretSetupKeepsValuesOutOfArguments(t *testing.T) {
	for _, file := range []string{"../docs/cluster-deployment.md", "../deploy/kubernetes/ghostline.yaml"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if strings.Contains(text, "--from-literal") {
			t.Errorf("%s passes secret values with --from-literal", file)
		}
		if !strings.Contains(text, "--from-env-file=/dev/stdin") {
			t.Errorf("%s must create ghostline-env from stdin", file)
		}
	}
}
