package parser

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
)

func TestSubscriptionCredentialBytesStructured(t *testing.T) {
	for _, protocol := range []string{"http", "socks5", "ss", "trojan", "hysteria", "hysteria2", "tuic", "ssh", "anytls", "naive"} {
		t.Run(protocol, func(t *testing.T) {
			node := map[string]any{
				"name": " proxy ", "type": protocol, "server": " 127.0.0.1 ", "port": 443,
				"password": " \tsecret\t ", "username": " user ", "sni": " example.com ",
			}
			want := map[string]string{"password": " \tsecret\t "}
			switch protocol {
			case "http", "socks5", "naive":
				want["username"] = " user "
			case "ss":
				node["cipher"] = " aes-128-gcm "
			case "hysteria":
				node["up"], node["down"], node["obfs"] = 10, 20, " obfs secret "
				want = map[string]string{"auth_str": " \tsecret\t ", "obfs": " obfs secret "}
			case "hysteria2":
				node["obfs"] = map[string]any{"type": "salamander", "password": " obfs secret "}
			case "tuic":
				node["uuid"] = "00000000-0000-0000-0000-000000000001"
			case "ssh":
				node["private-key-passphrase"] = " key secret "
				want["user"], want["private_key_passphrase"] = " user ", " key secret "
			}
			body, err := json.Marshal(map[string]any{"proxies": []any{node}})
			if err != nil {
				t.Fatal(err)
			}
			outbound := assertCredentialBytes(t, string(body), want)
			// Authentication bytes and ordinary configuration text have different
			// meanings: preserving the former must keep normal text cleanup.
			if outbound["server"] != "127.0.0.1" || outbound["tag"] != "proxy" {
				t.Fatalf("ordinary text was not normalized: %#v", outbound)
			}
			if protocol == "hysteria2" && outbound["obfs"].(map[string]any)["password"] != " obfs secret " {
				t.Fatalf("obfuscation credential changed: %#v", outbound["obfs"])
			}
		})
	}
	assertCredentialBytes(t, `proxies:
  - name: quoted
    type: hysteria2
    server: 127.0.0.1
    port: 443
    password: " secret "
    obfs: {type: salamander, password: " obfs secret "}
`, map[string]string{"password": " secret "})
	assertCredentialBytes(t, `proxies: [{name: spaces, type: hysteria2, server: 127.0.0.1, port: 443, password: "  "}]`, map[string]string{"password": "  "})
}

func TestSubscriptionCredentialBytesPlatform(t *testing.T) {
	tests := []struct {
		body string
		want map[string]string
	}{
		{`p = ss, 127.0.0.1, 443, aes-128-gcm, " secret "`, map[string]string{"password": " secret "}},
		{`p = ss, 127.0.0.1, 443, cipher=aes-128-gcm, password=" secret "`, map[string]string{"password": " secret "}},
		{`p = trojan, 127.0.0.1, 443, " secret "`, map[string]string{"password": " secret "}},
		{`p = hysteria2, 127.0.0.1, 443, password=" secret "`, map[string]string{"password": " secret "}},
		{`p = hysteria2, 127.0.0.1, 443, password="  "`, map[string]string{"password": "  "}},
		{`p = http, 127.0.0.1, 443, " user ", " secret "`, map[string]string{"username": " user ", "password": " secret "}},
		{`p = http, 127.0.0.1, 443, username=" user ", password=" secret "`, map[string]string{"username": " user ", "password": " secret "}},
		{`socks5=127.0.0.1:443, username=" user ", password=" secret ", tag=p`, map[string]string{"username": " user ", "password": " secret "}},
		{`shadowsocks=127.0.0.1:443, method=aes-128-gcm, password="  ", tag=p`, map[string]string{"password": "  "}},
		{`p = anytls, 127.0.0.1, 443, password=" secret "`, map[string]string{"password": " secret "}},
		{`p = tuic, 127.0.0.1, 443, 00000000-0000-0000-0000-000000000001, " secret "`, map[string]string{"password": " secret "}},
		{`p = ssh, 127.0.0.1, 443, " user ", " secret "`, map[string]string{"user": " user ", "password": " secret "}},
		{`p = http, 127.0.0.1, 443, "'user'", "'secret'"`, map[string]string{"username": "'user'", "password": "'secret'"}},
	}
	for index, tc := range tests {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			assertCredentialBytes(t, tc.body, tc.want)
		})
	}
}

func TestSubscriptionCredentialBytesURI(t *testing.T) {
	tests := []struct {
		body string
		want map[string]string
	}{
		{`hy2://%20secret%20@127.0.0.1:443/#p`, map[string]string{"password": " secret "}},
		{`hy2://127.0.0.1:443/?auth=%20secret%20#p`, map[string]string{"password": " secret "}},
		{`hy2://127.0.0.1:443/?password=%20%20#p`, map[string]string{"password": "  "}},
		{`hy2://user:@127.0.0.1:443/#p`, map[string]string{"password": "user:"}},
		{`trojan://%20secret%20@127.0.0.1:443/#p`, map[string]string{"password": " secret "}},
		{`anytls://%20secret%20@127.0.0.1:443/#p`, map[string]string{"password": " secret "}},
		{`anytls://user:@127.0.0.1:443/#p`, map[string]string{"password": "user:"}},
		{`http://%20user%20:%20secret%20@127.0.0.1:443/#p`, map[string]string{"username": " user ", "password": " secret "}},
		{`socks5://%20%20:%20%20@127.0.0.1:443/#p`, map[string]string{"username": "  ", "password": "  "}},
		{`tuic://00000000-0000-0000-0000-000000000001:%20secret%20@127.0.0.1:443/#p`, map[string]string{"password": " secret "}},
		{ssURI("aes-128-gcm", " secret ", "127.0.0.1", 443, "p"), map[string]string{"password": " secret "}},
	}
	for index, tc := range tests {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			assertCredentialBytes(t, tc.body, tc.want)
		})
	}
	// Hysteria authentication travels in a query value, so it also crosses
	// the generic options path before reaching the outbound producer.
	assertCredentialBytes(t, "hysteria://127.0.0.1:443/?auth="+url.QueryEscape(" secret ")+"&obfs="+url.QueryEscape(" obfs secret ")+"&upmbps=10&downmbps=20#p", map[string]string{"auth_str": " secret ", "obfs": " obfs secret "})
}

func assertCredentialBytes(t *testing.T, body string, want map[string]string) map[string]any {
	t.Helper()
	parsed, err := Parse(body)
	if err != nil || parsed.Skipped != 0 || len(parsed.Outbounds) != 1 {
		t.Fatalf("credential fixture failed to parse: err=%v result=%#v", err, parsed)
	}
	outbound := parsed.Outbounds[0]
	for key, expected := range want {
		if actual, ok := outbound[key].(string); !ok || actual != expected {
			t.Errorf("authentication field %q changed bytes: got %q, want %q", key, actual, expected)
		}
	}
	return outbound
}
