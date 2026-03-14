package blueprint_test

import (
	"strings"
	"testing"

	"github.com/home-operations/newt-sidecar/internal/blueprint"
	"github.com/home-operations/newt-sidecar/internal/config"
	"gopkg.in/yaml.v3"
)

func TestBlueprintYAMLOutput(t *testing.T) {
	cfg := &config.Config{
		SiteID:           "glistening-desert-rosy-boa",
		TargetHostname:   "kgateway-external.network.svc.cluster.local",
		TargetPort:       443,
		TargetMethod:     "https",
		DenyCountries:    "RU,CN,KP,IR,BY,IL",
		SSL:              true,
		AnnotationPrefix: "newt-sidecar",
	}

	bp := blueprint.Blueprint{
		PublicResources: map[string]blueprint.Resource{
			blueprint.HostnameToKey("home.erwanleboucher.dev"):   blueprint.BuildResource("home-assistant", "home.erwanleboucher.dev", nil, nil, cfg),
			blueprint.HostnameToKey("wsflux.erwanleboucher.dev"): blueprint.BuildResource("webhook-receiver", "wsflux.erwanleboucher.dev", nil, nil, cfg),
		},
	}

	data, err := yaml.Marshal(bp)
	if err != nil {
		t.Fatal(err)
	}

	out := string(data)
	t.Log("\n" + out)

	checks := []string{
		"public-resources:",
		"home-erwanleboucher-dev:",
		"name: home-assistant",
		"protocol: http",
		"ssl: true",
		"full-domain: home.erwanleboucher.dev",
		"tls-server-name: home.erwanleboucher.dev",
		"action: deny",
		"match: country",
		"value: RU",
		"site: glistening-desert-rosy-boa",
		"hostname: kgateway-external.network.svc.cluster.local",
		"method: https",
		"port: 443",
		"wsflux-erwanleboucher-dev:",
		"name: webhook-receiver",
	}

	for _, check := range checks {
		if !strings.Contains(out, check) {
			t.Errorf("output missing %q", check)
		}
	}
}
