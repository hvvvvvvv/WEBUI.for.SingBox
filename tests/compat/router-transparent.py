#!/usr/bin/env python3
"""Isolated namespace services/probes. No external DNS, URLs, or dependencies."""
import argparse
import collections
import http.client
import http.server
import ipaddress
import json
import pathlib
import secrets
import select
import socket
import socketserver
import struct
import threading
import time
import urllib.request

FOREIGN = "203.0.113.10"
CN = "198.51.100.20"
ROUTER_WAN = "10.204.0.1"
ORIGIN = "10.204.0.2"
RULES = {
    "Category-Ads": {"domain": ["ads.fixture.test"]},
    "GeoSite-Private": {"domain": ["private.fixture.test"]},
    "GeoSite-CN": {"domain": ["baidu.com"]},
    "GeoIP-Private": {"ip_cidr": ["192.0.2.0/24"]},
    "GeoIP-CN": {"ip_cidr": ["198.51.100.0/24"]},
    "GeoLocation-!CN": {"domain": ["google.com"]},
}
ALLOWED_CONNECT = {f"{FOREIGN}:8080", f"{CN}:8080", f"{ORIGIN}:1053", f"{ORIGIN}:8081"}
STATE = {"polluted": False, "connect": collections.Counter(), "downloads": collections.Counter(), "dns": [], "denied": []}
LOCK = threading.Lock()


def json_http(url, body=None, headers=None):
    # Ignore the runner's HTTP(S)_PROXY, including for namespace control calls.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(url, data=data, headers=headers or {})
    request.add_header("Content-Type", "application/json")
    with opener.open(request, timeout=5) as response:
        return json.loads(response.read(65536) or b"{}")


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_args):
        pass

    def send_json(self, value, status=200):
        body = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "close")
        self.end_headers()
        self.wfile.write(body)
        self.close_connection = True

    def do_GET(self):
        if self.server.server_port == 8080:
            self.send_json({"host": self.headers.get("Host"), "peer": self.client_address[0], "path": self.path})
        elif self.path == "/generate_204":
            self.send_response(204)
            self.send_header("Content-Length", "0")
            self.end_headers()
        elif self.path == "/metrics":
            with LOCK:
                snapshot = json.loads(json.dumps(STATE))
            self.send_json(snapshot)
        elif self.path.startswith("/rules/"):
            tag = self.path[len("/rules/"):].removesuffix(".json")
            if tag not in RULES:
                self.send_json({"error": "unknown rule set"}, 404)
                return
            with LOCK:
                STATE["downloads"][tag] += 1
            self.send_json({"version": 3, "rules": [RULES[tag]]})
        else:
            self.send_json({"error": "unknown fixture URL"}, 404)

    def do_POST(self):
        if self.server.server_port != 8081 or self.path not in ("/pollution/on", "/pollution/off"):
            self.send_json({"error": "unknown fixture operation"}, 404)
            return
        with LOCK:
            STATE["polluted"] = self.path.endswith("/on")
        self.send_json({"polluted": self.path.endswith("/on")})

    def do_CONNECT(self):
        if self.server.server_port != 3128 or self.path not in ALLOWED_CONNECT:
            with LOCK:
                STATE["denied"].append(self.path)
            self.send_json({"error": "only namespace fixture destinations are permitted"}, 403)
            return
        host, port = self.path.split(":")
        with LOCK:
            STATE["connect"][self.path] += 1
        with socket.create_connection((host, int(port)), timeout=5) as upstream:
            self.send_response(200)
            self.end_headers()
            self.wfile.flush()
            self.connection.setblocking(False)
            upstream.setblocking(False)
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                readable, _, _ = select.select([self.connection, upstream], [], [], 1)
                for source in readable:
                    data = source.recv(65536)
                    if not data:
                        self.close_connection = True
                        return
                    target = upstream if source is self.connection else self.connection
                    # Fixture payloads are tiny; bounded blocking writes avoid
                    # dropping a partial nonblocking send under CI load.
                    target.settimeout(5)
                    target.sendall(data)
                    target.setblocking(False)
            self.close_connection = True


def question(packet):
    if len(packet) < 17 or struct.unpack_from("!H", packet, 4)[0] != 1:
        raise ValueError("expected one DNS question")
    offset, labels = 12, []
    while packet[offset]:
        length = packet[offset]
        if length > 63 or offset + length + 1 >= len(packet):
            raise ValueError("invalid DNS label")
        labels.append(packet[offset + 1:offset + 1 + length].decode("ascii"))
        offset += length + 1
    offset += 1
    qtype, qclass = struct.unpack_from("!HH", packet, offset)
    return ".".join(labels).lower(), qtype, qclass, offset + 4


def answer(packet, peer, transport):
    domain, qtype, qclass, end = question(packet)
    with LOCK:
        known = domain in ("google.com", "baidu.com")
        address = CN if domain == "baidu.com" or STATE["polluted"] else FOREIGN
        STATE["dns"].append({"domain": domain, "address": address, "transport": transport, "peer": peer})
    count = int(known and qtype == 1 and qclass == 1)
    header = packet[:2] + struct.pack("!HHHHH", 0x8180 if known else 0x8183, 1, count, 0, 0)
    payload = b"\xc0\x0c" + struct.pack("!HHIH", 1, 1, 0, 4) + socket.inet_aton(address) if count else b""
    return header + packet[12:end] + payload


def read_exact(connection, length):
    result = b""
    while len(result) < length:
        chunk = connection.recv(length - len(result))
        if not chunk:
            raise EOFError("incomplete DNS TCP message")
        result += chunk
    return result


class UDPDNS(socketserver.BaseRequestHandler):
    def handle(self):
        packet, connection = self.request
        connection.sendto(answer(packet, self.client_address[0], "udp"), self.client_address)


class TCPDNS(socketserver.BaseRequestHandler):
    def handle(self):
        self.request.settimeout(5)
        while True:
            try:
                size = struct.unpack("!H", read_exact(self.request, 2))[0]
                if size > 4096:
                    raise ValueError("DNS fixture request too large")
                response = answer(read_exact(self.request, size), self.client_address[0], "tcp")
                self.request.sendall(struct.pack("!H", len(response)) + response)
            except (EOFError, TimeoutError, ConnectionError):
                return


def serve(root):
    servers = [http.server.ThreadingHTTPServer(("0.0.0.0", port), Handler) for port in (8080, 8081, 3128)]
    servers += [socketserver.ThreadingUDPServer(("0.0.0.0", 1053), UDPDNS), socketserver.ThreadingTCPServer(("0.0.0.0", 1053), TCPDNS)]
    for server in servers:
        server.daemon_threads = True
        threading.Thread(target=server.serve_forever, daemon=True).start()
    pathlib.Path(root, "fixture.ready").touch()
    threading.Event().wait()


def lookup(domain):
    transaction = secrets.token_bytes(2)
    name = b"".join(bytes([len(label)]) + label.encode("ascii") for label in domain.split(".")) + b"\x00"
    packet = transaction + struct.pack("!HHHHH", 0x0100, 1, 0, 0, 0) + name + struct.pack("!HH", 1, 1)
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as connection:
        connection.settimeout(5)
        # Nothing listens at this synthetic address. Only the real TUN DNS
        # hijack can provide a valid reply; a bypass cannot pass the test.
        connection.sendto(packet, ("192.0.2.53", 53))
        response, _ = connection.recvfrom(4096)
    if response[:2] != transaction or response[3] & 15 or struct.unpack_from("!H", response, 6)[0] < 1:
        raise AssertionError("invalid/no DNS answer through TUN")
    _, _, _, offset = question(response)
    if response[offset:offset + 2] != b"\xc0\x0c":
        while response[offset]:
            offset += response[offset] + 1
        offset += 1
    else:
        offset += 2
    kind, qclass, _ttl, size = struct.unpack_from("!HHIH", response, offset)
    if (kind, qclass, size) != (1, 1, 4):
        raise AssertionError("unexpected DNS record")
    return socket.inet_ntoa(response[offset + 10:offset + 14])


def request(domain, expected_address, expected_route, expected_dns, label):
    before = json_http(f"http://{ORIGIN}:8081/metrics")
    address = lookup(domain)
    if address != expected_address:
        raise AssertionError(f"controlled DNS returned {address}, expected {expected_address}")
    if not ipaddress.ip_address(address) in (ipaddress.ip_address(FOREIGN), ipaddress.ip_address(CN)):
        raise AssertionError("refusing any non-fixture destination")
    connection = http.client.HTTPConnection(address, 8080, timeout=5)
    connection.request("GET", "/" + label, headers={"Host": domain, "Connection": "close"})
    response = connection.getresponse()
    result = json.loads(response.read(4096))
    connection.close()
    after = json_http(f"http://{ORIGIN}:8081/metrics")
    if response.status != 200 or result.get("host") != domain or result.get("path") != "/" + label:
        raise AssertionError(f"wrong target response: {result}")
    destination = f"{address}:8080"
    proxied = after["connect"].get(destination, 0) - before["connect"].get(destination, 0)
    want_proxy = expected_route == "proxy"
    if proxied != int(want_proxy) or (result["peer"] == ROUTER_WAN) == want_proxy:
        raise AssertionError(f"wrong data path: target peer={result['peer']}, CONNECT delta={proxied}, expected {expected_route}")
    if result["peer"] == "10.203.0.2":
        raise AssertionError("client bypassed sing-box and was directly forwarded")
    new_dns = [item for item in after["dns"][len(before["dns"]):] if item["domain"] == domain]
    if not new_dns or any(item["transport"] != expected_dns for item in new_dns):
        raise AssertionError(f"wrong DNS route: {new_dns}, expected {expected_dns}")
    expected_dns_peer = ORIGIN if expected_dns == "tcp" else ROUTER_WAN
    if any(item["peer"] != expected_dns_peer for item in new_dns):
        raise AssertionError(f"DNS bypassed its configured outbound: {new_dns}, expected peer {expected_dns_peer}")
    if after["denied"]:
        raise AssertionError(f"unexpected proxy destinations: {after['denied']}")
    print(json.dumps({"case": label, "domain": domain, "dns_answer": address, "dns_transport": expected_dns,
                      "route": expected_route, "proxy_connects": proxied, "target_peer": result["peer"]}), flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["serve", "request", "pollution", "verify"])
    parser.add_argument("args", nargs="*")
    args = parser.parse_args()
    if args.command == "serve":
        serve(*args.args)
    elif args.command == "request":
        request(*args.args)
    elif args.command == "pollution":
        print(json.dumps(json_http("http://127.0.0.1:8081/pollution/" + args.args[0], {})))
    else:
        metrics = json_http("http://127.0.0.1:8081/metrics")
        if set(metrics["downloads"]) != set(RULES) or any(value != 1 for value in metrics["downloads"].values()) or metrics["denied"]:
            raise AssertionError(f"incomplete rule downloads or unexpected traffic: {metrics}")
        if metrics["connect"].get(f"{ORIGIN}:1053", 0) < 1:
            raise AssertionError("remote DNS never traversed the fixture HTTP proxy")
        print(json.dumps(metrics, indent=2))
