package config

import (
	"testing"

	profilev1 "guiforcores/gen/profile/v1"
)

func TestGenerateExperimentalUsesManagedCoreAPI(t *testing.T) {
	experimental := generateExperimental(&profilev1.Experimental{
		CacheFile: &profilev1.CacheFileExperimental{
			Enabled:     true,
			Path:        "cache.db",
			CacheId:     "cache-id",
			StoreFakeip: true,
			StoreRdrc:   true,
			RdrcTimeout: "7d",
		},
	}, nil)

	clashAPI, ok := experimental["clash_api"].(map[string]any)
	if !ok {
		t.Fatalf("expected generated clash_api object, got %#v", experimental["clash_api"])
	}
	if controller := clashAPI["external_controller"]; controller != CoreAPIController {
		t.Fatalf("expected managed controller %q, got %#v", CoreAPIController, controller)
	}
	secret, ok := clashAPI["secret"].(string)
	if !ok || len(secret) != 64 {
		t.Fatalf("expected generated 64-char secret, got %#v", clashAPI["secret"])
	}

	cacheFile, ok := experimental["cache_file"].(map[string]any)
	if !ok {
		t.Fatalf("expected generated cache_file object, got %#v", experimental["cache_file"])
	}
	if cacheFile["path"] != "cache.db" || cacheFile["cache_id"] != "cache-id" {
		t.Fatalf("expected profile cache_file values to be preserved, got %#v", cacheFile)
	}
}

func TestGenerateInboundsDirectNetwork(t *testing.T) {
	tests := []struct {
		name    string
		network profilev1.InboundNetwork
		want    string
	}{
		{
			name:    "tcp",
			network: profilev1.InboundNetwork_INBOUND_NETWORK_TCP,
			want:    "tcp",
		},
		{
			name:    "udp",
			network: profilev1.InboundNetwork_INBOUND_NETWORK_UDP,
			want:    "udp",
		},
		{
			name:    "unspecified defaults to udp",
			network: profilev1.InboundNetwork_INBOUND_NETWORK_UNSPECIFIED,
			want:    "udp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbounds := generateInbounds([]*profilev1.Inbound{
				{
					Id:     "direct-in",
					Type:   profilev1.InboundType_INBOUND_TYPE_DIRECT,
					Tag:    "direct-in",
					Enable: true,
					Direct: &profilev1.DirectInboundConfig{
						Listen: &profilev1.InboundListen{
							Listen:       "127.0.0.1",
							ListenPort:   20123,
							TcpFastOpen:  true,
							TcpMultiPath: true,
							UdpFragment:  true,
						},
						Network: tt.network,
					},
				},
			})

			if len(inbounds) != 1 {
				t.Fatalf("expected one inbound, got %#v", inbounds)
			}
			item, ok := inbounds[0].(map[string]any)
			if !ok {
				t.Fatalf("expected inbound map, got %#v", inbounds[0])
			}
			if item["type"] != "direct" || item["tag"] != "direct-in" {
				t.Fatalf("expected direct inbound metadata, got %#v", item)
			}
			if item["network"] != tt.want {
				t.Fatalf("expected network %q, got %#v", tt.want, item["network"])
			}
			if _, ok := item["users"]; ok {
				t.Fatalf("direct inbound should not include users, got %#v", item)
			}
			if item["listen"] != "127.0.0.1" || item["listen_port"] != int32(20123) {
				t.Fatalf("expected listen fields to be preserved, got %#v", item)
			}
			if item["tcp_fast_open"] != true || item["tcp_multi_path"] != true || item["udp_fragment"] != true {
				t.Fatalf("expected listen options to be preserved, got %#v", item)
			}
		})
	}
}
