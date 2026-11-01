#!/usr/bin/env python3
"""Controlled DNS forwarders for the real Linux TUN matrix; no AGH implementation.

LAN:53 -> loopback:5353 -> WAN:53 models dnsmasq/AdGuard-style forwarding.
The core uses a distinct :1053 resolver. TTL=0 and unique names expose each hop.
No filtering, encrypted DNS, firmware firewall, IPv6, or real Internet is modeled.
"""
import argparse
import http.client
import http.server
import importlib.util
import json
import pathlib
import secrets
import socket
import socketserver
import struct
import threading

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("transparent_fixture", ROOT / "compat/router-transparent.py")
FIXTURE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(FIXTURE)
LAN = "10.203.0.1"
UPSTREAM = "10.204.0.2"
ATTACHED = "10.205.0.2"
FORWARDED = "203.0.113.10"
INTERCEPTED = "198.51.100.20"
LOCK = threading.Lock()


def question(packet):
    if len(packet) < 17 or struct.unpack_from("!H", packet, 4)[0] != 1:
        raise ValueError("expected one uncompressed question")
    offset, labels = 12, []
    while True:
        if offset >= len(packet):
            raise ValueError("truncated DNS name")
        size = packet[offset]
        offset += 1
        if size == 0:
            break
        if size > 63 or offset + size > len(packet):
            raise ValueError("invalid DNS label")
        labels.append(packet[offset:offset + size].decode("ascii"))
        offset += size
    if offset + 4 > len(packet):
        raise ValueError("truncated DNS question")
    kind, family = struct.unpack_from("!HH", packet, offset)
    return ".".join(labels).lower(), kind, family, offset + 4


def query(name):
    encoded = b"".join(bytes([len(label)]) + label.encode("ascii") for label in name.split(".")) + b"\0"
    return secrets.token_bytes(2) + struct.pack("!HHHHH", 0x0100, 1, 0, 0, 0) + encoded + struct.pack("!HH", 1, 1)


def answer(packet, address):
    name, kind, family, end = question(packet)
    if not name.endswith(".fixture.test") or kind != 1 or family != 1:
        raise ValueError("only controlled fixture A queries are allowed")
    return (packet[:2] + struct.pack("!HHHHH", 0x8180, 1, 1, 0, 0) + packet[12:end]
            + b"\xc0\x0c" + struct.pack("!HHIH", 1, 1, 0, 4) + socket.inet_aton(address))


def address_from_response(packet, response):
    if len(response) < 12 or response[:2] != packet[:2] or response[3] & 15 or not response[2] & 0x80:
        raise AssertionError("invalid DNS transaction or error response")
    name, kind, family, offset = question(response)
    if (name, kind, family) != question(packet)[:3] or struct.unpack_from("!H", response, 6)[0] != 1:
        raise AssertionError("wrong DNS question/answer count")
    if response[offset:offset + 2] == b"\xc0\x0c":
        offset += 2
    else:
        while offset < len(response) and response[offset]:
            size = response[offset]
            if size > 63:
                raise AssertionError("unexpected DNS answer compression")
            offset += size + 1
        offset += 1
    if offset + 14 > len(response):
        raise AssertionError("truncated DNS answer")
    kind, family, _ttl, size = struct.unpack_from("!HHIH", response, offset)
    if (kind, family, size) != (1, 1, 4):
        raise AssertionError("expected IPv4 A answer")
    return socket.inet_ntoa(response[offset + 10:offset + 14])


def exchange(packet, destination, transport):
    if transport == "udp":
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as connection:
            connection.settimeout(3)
            connection.connect(destination)
            connection.send(packet)
            return connection.recv(4096)
    with socket.create_connection(destination, timeout=3) as connection:
        connection.sendall(struct.pack("!H", len(packet)) + packet)
        size = struct.unpack("!H", FIXTURE.read_exact(connection, 2))[0]
        if size > 4096:
            raise ValueError("oversized DNS TCP response")
        return FIXTURE.read_exact(connection, size)


def record(root, role, value):
    with LOCK, (root / (role + ".jsonl")).open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(value) + "\n")


def handle(server, packet, peer, transport):
    domain, _kind, _family, _end = question(packet)
    if not domain.endswith(".fixture.test"):
        raise ValueError("refusing query outside fixture.test")
    record(server.root, server.role, {"domain": domain, "transport": transport, "peer": peer,
                                     "listener": list(server.server_address)})
    if server.upstream:
        return exchange(packet, server.upstream, transport)
    return answer(packet, server.answer)


class UDP(socketserver.BaseRequestHandler):
    def handle(self):
        packet, connection = self.request
        connection.sendto(handle(self.server, packet, self.client_address[0], "udp"), self.client_address)


class TCP(socketserver.BaseRequestHandler):
    def handle(self):
        self.request.settimeout(3)
        while True:
            try:
                size = struct.unpack("!H", FIXTURE.read_exact(self.request, 2))[0]
                if size > 4096:
                    raise ValueError("oversized DNS TCP query")
                response = handle(self.server, FIXTURE.read_exact(self.request, size), self.client_address[0], "tcp")
                self.request.sendall(struct.pack("!H", len(response)) + response)
            except (EOFError, TimeoutError, ConnectionError):
                return


def serve(root, location):
    servers = []
    if location == "origin":
        # Reuse only the existing bounded HTTP proxy/target and six rule fixtures.
        servers += [http.server.ThreadingHTTPServer(("0.0.0.0", port), FIXTURE.Handler)
                    for port in (8080, 8081, 3128)]
        # Bind each address separately: an unbound UDP reply could otherwise
        # choose the WAN source for a query arriving on the attached subnet.
        listeners = [("upstream53", UPSTREAM, 53, None, FORWARDED),
                     ("upstream53", ATTACHED, 53, None, FORWARDED),
                     ("core1053", UPSTREAM, 1053, None, INTERCEPTED)]
    else:
        listeners = [("dnsmasq", LAN, 53, ("127.0.0.1", 5353), None),
                     ("adguard", "127.0.0.1", 5353, (UPSTREAM, 53), None)]
    for role, host, port, upstream, result in listeners:
        for server_type, handler in ((socketserver.ThreadingUDPServer, UDP), (socketserver.ThreadingTCPServer, TCP)):
            server = server_type((host, port), handler)
            server.root, server.role, server.upstream, server.answer = root, role, upstream, result
            servers.append(server)
    for server in servers:
        server.daemon_threads = True
        threading.Thread(target=server.serve_forever, daemon=True).start()
    (root / (location + ".ready")).touch()
    threading.Event().wait()


def expected(mode, redirect, path):
    # Pinned 1.14.4 source: without auto_redirect, dport53 policy rules are
    # unconditional. With nftables, the LAN DNS DNAT precedes local exclusions.
    intercepted = path == "attached" and (not redirect or mode == "hijack")
    intercepted |= path == "lan" and redirect and mode == "hijack"
    if intercepted:
        return INTERCEPTED, ["core1053"]
    return FORWARDED, {"lan": ["dnsmasq", "adguard", "upstream53"],
                       "loopback": ["adguard", "upstream53"], "attached": ["upstream53"]}[path]


def probe(root, mode, redirect, path, transport):
    destination = {"lan": (LAN, 53), "loopback": ("127.0.0.1", 5353), "attached": (ATTACHED, 53)}[path]
    domain = f"{mode}-{int(redirect)}-{path}-{transport}-{secrets.token_hex(4)}.fixture.test"
    packet = query(domain)
    address = address_from_response(packet, exchange(packet, destination, transport))
    wanted, hops = expected(mode, redirect, path)
    if address != wanted:
        raise AssertionError(f"{domain}: answer {address}, expected {wanted}; inspect per-hop logs")
    connection = http.client.HTTPConnection(address, 8080, timeout=4)
    try:
        connection.request("GET", "/" + domain, headers={"Host": domain})
        response = connection.getresponse()
        body = json.loads(response.read(65536))
        if response.status != 200 or body.get("host") != domain or body.get("path") != "/" + domain:
            raise AssertionError("resolved address did not reach the controlled HTTP target")
    finally:
        connection.close()
    sample = {"domain": domain, "mode": mode, "auto_redirect": redirect, "path": path,
              "transport": transport, "answer": address, "expected_hops": hops, "http": body}
    record(root, "assertions", sample)
    print(json.dumps(sample), flush=True)


def prepare(source, root, mode, redirect):
    config = json.loads(source.read_text(encoding="utf-8"))
    tun = next(item for item in config["inbounds"] if item["type"] == "tun")
    tun.update(dns_mode=mode, auto_redirect=redirect)
    # Original WAN exclusion keeps the chained upstream independent; the third
    # connected subnet deliberately is not excluded, exposing the port53 rules.
    assert tun["route_exclude_address"] == ["10.204.0.0/24"]
    assert tun["include_interface"] == ["lan0"]
    config["experimental"]["cache_file"]["path"] = str(root / "cache.db")
    config["dns"]["disable_cache"] = True
    root.mkdir(parents=True, exist_ok=False)
    (root / "config.json").write_text(json.dumps(config, indent=2), encoding="utf-8")
    for service in config.get("services", []):
        if "secret" in service:
            service["secret"] = "[redacted]"
    (root / "config.redacted.json").write_text(json.dumps(config, indent=2), encoding="utf-8")


def verify(root):
    samples = [json.loads(line) for line in (root / "assertions.jsonl").read_text().splitlines()]
    assert len(samples) == 6 and len({(s["path"], s["transport"]) for s in samples}) == 6
    events = {}
    for role in ("dnsmasq", "adguard", "upstream53", "core1053"):
        file = root / (role + ".jsonl")
        events[role] = [json.loads(line) for line in file.read_text().splitlines()] if file.exists() else []
    for sample in samples:
        seen = {role: [e for e in entries if e["domain"] == sample["domain"]] for role, entries in events.items()}
        assert set(role for role, entries in seen.items() if entries) == set(sample["expected_hops"]), seen
        for role in sample["expected_hops"]:
            assert len(seen[role]) == 1, seen
            if role != "core1053":  # core's configured remote DNS is always TCP
                assert seen[role][0]["transport"] == sample["transport"], seen
            else:
                assert seen[role][0]["transport"] == "tcp", seen
                assert seen[role][0]["peer"] == UPSTREAM, "Core DNS bypassed its HTTP proxy detour"
        if "adguard" in sample["expected_hops"]:
            assert seen["adguard"][0]["peer"] == "127.0.0.1", seen
        if "dnsmasq" in sample["expected_hops"]:
            assert seen["dnsmasq"][0]["peer"] == "10.203.0.2", seen
        if "upstream53" in sample["expected_hops"]:
            peer = "10.203.0.2" if sample["path"] == "attached" else "10.204.0.1"
            assert seen["upstream53"][0]["peer"] == peer, seen
    before = json.loads((root / "tun.before.json").read_text())[0]
    after = json.loads((root / "tun.after.json").read_text())[0]
    assert after["linkinfo"]["info_kind"] == "tun"
    rx = lambda link: link.get("stats64", link.get("stats", {}))["rx"]["packets"]
    assert rx(after) > rx(before), "No packets crossed the real TUN"
    summary = {"mode": samples[0]["mode"], "auto_redirect": samples[0]["auto_redirect"],
               "assertions": len(samples), "tun_rx_before": rx(before), "tun_rx_after": rx(after),
               "scope": "IPv4 UDP/TCP; synthetic DNS forwarders, not real AdGuard Home filters"}
    (root / "evidence.log").write_text(json.dumps(summary) + "\n" + json.dumps(samples, indent=2) + "\n")
    print(json.dumps(summary))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("prepare", "serve", "probe", "verify"))
    parser.add_argument("root", type=pathlib.Path)
    parser.add_argument("args", nargs="*")
    args = parser.parse_args()
    if args.action == "prepare":
        prepare(pathlib.Path(args.args[0]), args.root, args.args[1], args.args[2] == "true")
    elif args.action == "serve":
        serve(args.root, args.args[0])
    elif args.action == "probe":
        probe(args.root, args.args[0], args.args[1] == "true", args.args[2], args.args[3])
    else:
        verify(args.root)
